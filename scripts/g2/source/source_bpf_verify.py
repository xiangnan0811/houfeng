"""Independent, fail-closed host checks for the isolated BPF transcript.

The transcript hash authenticates ordering, not the underlying kernel facts.
Successful admission additionally requires independent replay of the complete
policy and lifetime evidence; see the explicit failure at the verification
boundary below when that evidence is not available to the host.
"""
import base64
import binascii
import hashlib
import json
import os
import re
import stat
from pathlib import Path
import struct
import tarfile


_ZERO = '0' * 64
_MAX_TRANSCRIPT_BYTES = 16777216
_MAX_EVIDENCE_BYTES = 33554432
_MAX_RESULT_BYTES = 67108864
_MAX_AUDIT_WIRE_IDENTITIES = 16384
_U64 = (1 << 64) - 1
_I64_MIN = -(1 << 63)
_I64_MAX = (1 << 63) - 1
_HEX64 = re.compile(r'[0-9a-f]{64}\Z')
_HEX32 = re.compile(r'[0-9a-f]{32}\Z')
_UUID = re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z')
_COMMAND_IDS = {'MAP_CREATE': 0, 'MAP_LOOKUP_ELEM': 1, 'MAP_UPDATE_ELEM': 2, 'MAP_DELETE_ELEM': 3,
                'PROG_LOAD': 5, 'PROG_ATTACH': 8, 'PROG_DETACH': 9, 'PROG_GET_FD_BY_ID': 13,
                'OBJ_GET_INFO_BY_FD': 15, 'PROG_QUERY': 16, 'BTF_LOAD': 18, 'MAP_FREEZE': 22, 'LINK_CREATE': 28, 'LINK_DETACH': 34}
_AUTHORIZATION_NAMES = frozenset((
    'allowed-device-start', 'wrong-actor', 'wrong-program-byte', 'wrong-flags', 'wrong-target', 'wrong-generation',
    'nested-buffer-race', 'fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse',
    'feature-map', 'extended-info-ebadf', 'replace-probe-ebadf', 'audit-interleaved', 'audit-loss',
    'audit-duplicate', 'audit-overflow', 'container-bpf', 'commit-before-cancel', 'after-effect-cancel',
    'after-reply-cancel', 'seal-gap-death', 'seal-detach-denied', 'seal-unload-denied', 'attachment-replace',
    'failure-cleanup', 'cgroup-auto-retire', 'extra-fd-delays-removal', 'id-reuse', 'snapshot-missing',
    'snapshot-bad-field', 'guard-overflow', 'guard-miss', 'guard-pairing', 'fd-arm-map', 'fd-arm-program',
    'fd-arm-replay', 'fd-arm-wrong-case', 'fd-arm-wrong-role', 'fd-arm-wrong-grant', 'fd-arm-second',
    'fd-fill-failure', 'fd-restore-failure',
))
_BASELINE_OBSERVATION_NAMES = frozenset((
    'allowed-device-start', 'wrong-actor', 'wrong-program-byte', 'wrong-flags', 'wrong-target', 'wrong-generation',
    'nested-buffer-race', 'fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse',
    'feature-map', 'extended-info-ebadf', 'replace-probe-ebadf', 'audit-interleaved',
    'container-bpf', 'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel',
    'seal-detach-denied', 'seal-unload-denied', 'attachment-replace', 'failure-cleanup',
    'cgroup-auto-retire', 'extra-fd-delays-removal', 'id-reuse',
    'fd-arm-map', 'fd-arm-program', 'fd-arm-replay', 'fd-arm-wrong-role', 'fd-arm-wrong-grant', 'fd-arm-second',
))
_TERMINAL_FAULT_VARIANTS = frozenset((
    'audit-loss', 'audit-duplicate', 'audit-overflow', 'snapshot-missing', 'snapshot-bad-field',
    'guard-overflow', 'guard-miss', 'guard-pairing', 'fd-fill-failure', 'fd-restore-failure',
))
_EARLY_SEAL_VARIANTS = frozenset((
    'seal-guard-loaded', 'seal-inode-links', 'seal-bpf-link', 'seal-sealed', 'seal-audit-locked',
))
_READER_FAULT_VARIANTS = frozenset((
    'reader-death', 'reader-endpoint', 'reader-timeout', 'reader-replay',
    'reader-credential', 'reader-extra-fd', 'reader-frame', 'reader-done',
))
_RECORD_KEYS = {
    'principal': 'id role task_cookie tgid tid start_ns parent_cookie exe_sha256 script_sha256 argv_sha256 uid gid cgroup_sha256 registration terminal',
    'runtime_root': 'broker_cookie pid1_cookie pid1_start_ns pid1_exe_sha256 root_mount_id root_dev root_ino handoff_count thread_ids',
    'containerd_launcher': 'path exe_sha256 exe_dev exe_ino target_path target_sha256 target_dev target_ino',
    'grant': 'id owner purpose container_id config config_sha256 invocation phase retirement',
    'invocation': 'id principal_id task_cookie daemon_cookie grant_id container_id generation command config_sha256 bundle_sha256 process_sha256 exec_id listener_id ancestry state',
    'daemon': 'cookie owner mode task_cookie start_ns helper_sha256 dockerd_sha256 argv_sha256 watch_sequence state',
    'transaction': 'id grant_id invocation_id requester_cookie notification_id command attributes program_bytes license input_objects executor_cookie state result returned_fd output cleanup',
    'object': 'cookie class kernel_id owner_transaction type attributes attribute_source bytes publication_seq removal_seq state',
    'attachment': 'id grant_id enrollment root_cookie cgroup_id program_cookie attach_type flags replaces transaction_id offline_seq release_seq state',
    'audit': 'stamp serial record_type context object_cookie transaction_id task_cookie command result operation raw',
    'audit_wire': 'stamp serial record_type payload_sha256 occurrence',
    'selftest_audit_reorder': 'name nonce received delivery_order',
    'audit_cut': 'principal_id readback_sequence boundary_sequence boundary_sha256 wire_count',
    'audit_control': 'stamp serial actor phase change old_value new_value rule_list result',
    'audit_readback': 'reader_cookie request_id nonce check queries',
    'checkpoint': 'nonce begin_watermark end_watermark tail_before principal_id state',
    'guard': 'record',
    'registry': 'map action key value grant_id transaction_id parent_transaction_id',
    'selftest_fd_stage': 'nonce grant_id class transaction_id phase saved_soft hard_limit test_soft original_fd_count identity_fd_count owned_fd_count errno',
    'selftest_runtime_stage': 'nonce grant_id invocation_id transaction_id mode phase notifying_cookie notification_id mutation signal',
    'notification_denial': 'invocation_id requester_cookie notification_id command result guard_before guard_after',
    'notification_cancel': 'invocation_id requester_cookie notification_id command validation_errno guard_before guard_after',
    'notification_response': 'transaction_id invocation_id requester_cookie notification_id operation response ioctl_errno returned_fd guard_before guard_after',
    'cgroup_enrollment': 'grant_id enrollment root_cookie cgroup_id invocation_id requester_cookie notification_id',
    'held_file': 'cookie kind source device inode rdev mode uid gid invocation_id requester_cookie notification_id requester_fd baseline_cookie',
    'grant_object_cleanup': 'grant_id creator_transaction_id object_cookie step action result guard_sequence',
    'runtime_exec': 'invocation_id task_cookie exec_generation guard_sequence exe_sha256',
    'module_instance': 'instance loader_cookie module_sha256 load_file observer_bindings_sha256',
    'probe_actor': 'name probe_cookie task_cookie tgid tid start_ns parent_cookie exe_sha256 listener_id',
    'probe_import': 'probe_cookie notifying_cookie notification_id slot imported_device imported_inode fixture guard_begin guard_end result',
    'exec_admission': 'exec_id grant_id observation endpoint_generation docker_exec_id task_cookie process_sha256 state endpoint',
    'endpoint_transition': 'transition_id grant_id recovery_id coordinator_cookie scope_sha256 endpoint_generation state endpoint',
    'selftest_case': 'name nonce phase actor_cookie invocation_id transaction_ids guard_sequences reference_cookies request_sha256 result error_code',
    'selftest_guard_status': 'name nonce phase status',
    'control_rejection': 'principal_id request_id operation nonce body request_sha256 status code guard_before guard_after',
}
_RECORD_KEYS = {key: frozenset(value.split()) for key, value in _RECORD_KEYS.items()}
_ENVELOPE = frozenset('version boot_id epoch sequence previous kind body'.split())
_BOOTSTRAP = frozenset('version boot_id epoch boot_inputs_sha256 bootstrap_manifest_sha256 observer_bindings_sha256 broker_sha256 guard_sha256 policy_sha256 generator_sha256 links broker_principal bootstrap_tail records'.split())
_SUFFIX = frozenset('version boot_id epoch genesis_tail records final_watermark final_tail guard_status'.split())
_STANDALONE = frozenset(('observer-capability', 'genesis-missing', 'late-genesis', 'bpf-authorization', 'bpf-broker-kill'))
_DAEMON_HELPER_ARGV = ('/opt/g2/g2-bpf-authority', 'daemon-child')
_DOCKERD_ARGV = (
    '/usr/bin/dockerd', '--host=unix:///var/run/docker.sock', '--group=g2coord',
    '--data-root=/var/lib/g2/docker', '--exec-root=/run/g2-docker', '--storage-driver=overlay2',
    '--bridge=none', '--iptables=false', '--ip6tables=false', '--ip-forward=false',
    '--ip-masq=false', '--userland-proxy=false', '--log-driver=none',
    '--default-address-pool=base=172.29.0.0/16,size=24',
    '--add-runtime=g2-bpf=/opt/g2/g2-bpf-runtime', '--default-runtime=g2-bpf',
    '--exec-opt=native.cgroupdriver=cgroupfs')
_STATES = {
    'principal': {'role': ('BOOT_OWNER', 'REALM_ROOT', 'RECOVERY_PENDING', 'RECOVERY_OBSERVER', 'RUNTIME', 'BOOTSTRAP_BROKER', 'BOOTSTRAP_AUDIT_READER', 'BOOTSTRAP_TERMINAL_OBSERVER'),
                  'registration': ('launch_source', 'launch_reboot', 'realm_self', 'realm_child', 'recovery', 'runtime', 'bootstrap'),
                  'terminal': (None, 'exit', 'revoked')},
    'grant': {'phase': ('admitted', 'active', 'settling', 'retired', 'revoked'),
              'retirement': ('auto_empty', 'explicit_cleanup')},
    'invocation': {'command': ('features', 'create', 'start', 'state', 'kill', 'delete', 'exec'),
                   'state': ('bound', 'running', 'settled', 'failed')},
    'daemon': {'mode': ('initial', 'reboot'), 'state': ('stopped', 'executing', 'exited')},
    'transaction': {'state': ('prepared', 'committed', 'effect_observed', 'response_settled', 'retired')},
    'object': {'class': ('program', 'map'), 'state': ('live', 'id_removed', 'reclaim_pending')},
    'attachment': {'state': ('attached', 'detached', 'offlining', 'released')},
    'audit': {'context': ('syscall', 'standalone'), 'record_type': ('SYSCALL', 'BPF')},
    'checkpoint': {'state': ('quiesced', 'released', 'failed')},
    'registry': {'map': ('broker_token', 'guard_liveness', 'realm_targets', 'retire_tasks'), 'action': ('insert', 'update', 'delete')},
    'exec_admission': {'state': ('admitted', 'bound', 'settled', 'cancelled')},
    'endpoint_transition': {'state': ('begun', 'completed', 'closed')},
}
_IDS = {
    'principal': ('id', 'task_cookie', 'tgid', 'tid', 'start_ns'),
    'grant': ('id', 'owner', 'invocation'),
    'invocation': ('id', 'principal_id', 'task_cookie', 'daemon_cookie', 'generation'),
    'daemon': ('cookie', 'owner', 'task_cookie', 'start_ns', 'watch_sequence'),
    'transaction': ('id', 'executor_cookie'),
    'object': ('cookie', 'kernel_id', 'owner_transaction', 'publication_seq'),
    'attachment': ('id', 'grant_id', 'enrollment', 'root_cookie', 'cgroup_id', 'program_cookie', 'transaction_id'),
    'cgroup_enrollment': ('grant_id', 'enrollment', 'root_cookie', 'cgroup_id', 'invocation_id', 'requester_cookie', 'notification_id'),
    'held_file': ('cookie', 'inode'), 'grant_object_cleanup': ('grant_id', 'creator_transaction_id', 'object_cookie'),
    'runtime_exec': ('invocation_id', 'task_cookie', 'exec_generation', 'guard_sequence'),
    'notification_response': ('transaction_id', 'invocation_id', 'requester_cookie', 'notification_id'),
    'checkpoint': ('principal_id',), 'registry': ('transaction_id',),
    'module_instance': ('instance', 'loader_cookie'),
    'probe_actor': ('probe_cookie', 'task_cookie', 'tgid', 'tid', 'start_ns', 'parent_cookie'),
    'probe_import': ('probe_cookie', 'notification_id'),
    'exec_admission': ('exec_id', 'grant_id'),
    'endpoint_transition': ('transition_id', 'grant_id', 'coordinator_cookie'),
}
_NULLABLE_IDS = {
    'principal': ('parent_cookie',), 'invocation': ('grant_id', 'exec_id', 'listener_id'),
    'transaction': ('grant_id', 'invocation_id', 'requester_cookie', 'notification_id'),
    'object': ('removal_seq',), 'attachment': ('replaces', 'offline_seq', 'release_seq'),
    'registry': ('parent_transaction_id',),
    'held_file': ('invocation_id', 'requester_cookie', 'notification_id', 'baseline_cookie'),
    'grant_object_cleanup': ('guard_sequence',),
    'audit': ('object_cookie', 'transaction_id', 'task_cookie'),
    'probe_actor': ('listener_id',),
    'probe_import': ('notifying_cookie', 'imported_device', 'imported_inode'),
    'selftest_runtime_stage': ('invocation_id', 'transaction_id', 'notifying_cookie', 'notification_id'),
    'exec_admission': ('endpoint_generation', 'task_cookie'),
    'endpoint_transition': ('endpoint_generation',),
}


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii')


def _hash(value):
    return hashlib.sha256(_canonical(value)).hexdigest()


def _integer(value, low=0, high=_U64):
    return type(value) is int and low <= value <= high


def _hex(value, pattern=_HEX64, nonzero=True):
    return type(value) is str and pattern.fullmatch(value) is not None and (not nonzero or int(value, 16) != 0)


def _shape(value, fields):
    return type(value) is dict and set(value) == set(fields)
def _audit_stamp(host, body):
    stamp = body['stamp']
    host.require(_shape(stamp, ('seconds', 'milliseconds')) and
                 _integer(stamp['seconds'], high=_I64_MAX) and
                 _integer(stamp['milliseconds'], high=999) and
                 _integer(body['serial'], 1, (1 << 32) - 1), 'BPF audit stamp invalid')
    return stamp['seconds'], stamp['milliseconds'], body['serial']




def _json(host, value, depth=0):
    host.require(depth <= 64, 'BPF evidence JSON nesting exceeds limit')
    if value is None or type(value) in (bool, str):
        return
    if type(value) is int:
        host.require(_integer(value, _I64_MIN), 'BPF evidence integer exceeds wire range')
        return
    if type(value) is list:
        for item in value:
            _json(host, item, depth + 1)
        return
    host.require(type(value) is dict and all(type(key) is str for key in value), 'BPF evidence JSON type invalid')
    for item in value.values():
        _json(host, item, depth + 1)


def _record_body(host, kind, body):
    host.require(_shape(body, _RECORD_KEYS[kind]), 'BPF %s record schema differs' % kind)
    for field, allowed in _STATES.get(kind, {}).items():
        host.require(body[field] in allowed, 'BPF %s %s is invalid' % (kind, field))
    host.require(all(_integer(body[field], 1) for field in _IDS.get(kind, ())), 'BPF %s identity invalid' % kind)
    host.require(all(body[field] is None or _integer(body[field], 1) for field in _NULLABLE_IDS.get(kind, ())),
                 'BPF %s nullable identity invalid' % kind)
    if kind == 'principal':
        host.require(all(_hex(body[field]) for field in ('exe_sha256', 'argv_sha256', 'cgroup_sha256')) and
                     (body['script_sha256'] is None or _hex(body['script_sha256'])) and
                     all(_integer(body[field], high=(1 << 32) - 1) for field in ('uid', 'gid', 'tgid', 'tid')),
                     'BPF principal identity invalid')
    elif kind == 'runtime_root':
        tids = body['thread_ids']
        host.require(all(_integer(body[field], 1) for field in
                         ('broker_cookie', 'pid1_cookie', 'pid1_start_ns', 'root_mount_id', 'root_ino')) and
                     _integer(body['root_dev']) and _integer(body['handoff_count']) and body['handoff_count'] == 1 and
                     _hex(body['pid1_exe_sha256']) and type(tids) is list and 1 <= len(tids) <= 4096 and
                     all(_integer(tid, 1, (1 << 32) - 1) for tid in tids) and
                     all(a < b for a, b in zip(tids, tids[1:])), 'BPF runtime root record invalid')
    elif kind == 'containerd_launcher':
        host.require(body['path'] == '/opt/g2/containerd-bin/containerd' and
                     body['target_path'] == '/usr/bin/containerd' and
                     _hex(body['exe_sha256']) and _hex(body['target_sha256']) and
                     all(_integer(body[field]) for field in ('exe_dev', 'target_dev')) and
                     all(_integer(body[field], 1) for field in ('exe_ino', 'target_ino')) and
                     (body['exe_dev'], body['exe_ino']) != (body['target_dev'], body['target_ino']),
                     'BPF containerd launcher binding invalid')
    elif kind == 'grant':
        host.require(type(body['purpose']) is str and type(body['config']) is dict and
                     _hex(body['container_id']) and _hex(body['config_sha256']) and
                     _hash(body['config']) == body['config_sha256'], 'BPF grant config invalid')
    elif kind == 'daemon':
        host.require(all(_hex(body[field]) for field in ('helper_sha256', 'dockerd_sha256', 'argv_sha256')),
                     'BPF daemon digest invalid')
    elif kind == 'invocation':
        for field in ('config_sha256', 'bundle_sha256', 'process_sha256'):
            host.require(body[field] is None or _hex(body[field]), 'BPF invocation digest invalid')
        host.require(type(body['ancestry']) is list and body['ancestry'], 'BPF invocation ancestry missing')
        for node in body['ancestry']:
            host.require(_shape(node, ('task_cookie', 'parent_cookie', 'exec_generation', 'exe_sha256', 'fork_sequence', 'exec_sequence')) and
                         all(_integer(node[field], 1) for field in ('task_cookie', 'exec_generation', 'fork_sequence', 'exec_sequence')) and
                         _integer(node['parent_cookie']) and _hex(node['exe_sha256']),
                         'BPF invocation ancestry invalid')
        if body['command'] == 'features':
            host.require(all(body[field] is None for field in ('grant_id', 'container_id', 'config_sha256', 'bundle_sha256', 'process_sha256', 'exec_id', 'listener_id')),
                         'BPF features invocation acquired authority')
        else:
            host.require(_integer(body['grant_id'], 1) and _integer(body['listener_id'], 1) and
                         _hex(body['container_id']) and _hex(body['config_sha256']) and
                         ((body['command'] == 'create') == (body['bundle_sha256'] is not None)) and
                         ((body['command'] == 'exec') == (body['process_sha256'] is not None and body['exec_id'] is not None)),
                         'BPF runtime invocation binding invalid')
    elif kind == 'transaction':
        attr, cleanup = body['attributes'], body['cleanup']
        host.require(_shape(attr, ('attr_size', 'attr_hex')) and _integer(attr['attr_size'], high=4096) and
                     type(attr['attr_hex']) is str and re.fullmatch(r'[0-9a-f]*', attr['attr_hex']) is not None and
                     len(attr['attr_hex']) == attr['attr_size'] * 2 and type(body['input_objects']) is list and
                     _shape(cleanup, ('policy', 'state', 'steps')) and
                     cleanup['policy'] in ('none', 'close_unattached', 'detach_then_close', 'auto_empty', 'close_borrowed_ref') and
                     cleanup['state'] in ('not_needed', 'pending', 'settled', 'unknown') and type(cleanup['steps']) is list,
                     'BPF transaction wire data invalid')
        commands = ('MAP_CREATE', 'PROG_LOAD', 'PROG_QUERY', 'PROG_GET_FD_BY_ID', 'OBJ_GET_INFO_BY_FD',
                    'PROG_ATTACH', 'PROG_DETACH', 'BTF_LOAD', 'LINK_CREATE', 'MAP_LOOKUP_ELEM',
                    'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'MAP_FREEZE')
        host.require(type(body['command']) is str and body['command'] in commands and
                     (body['result'] is None or _integer(body['result'], _I64_MIN, _I64_MAX)) and
                     (body['returned_fd'] is None or _integer(body['returned_fd'], high=(1 << 31) - 1)) and
                     (body['output'] is None or type(body['output']) is dict), 'BPF transaction outcome invalid')
        raw = body['program_bytes']
        host.require(raw is None or type(raw) is str and re.fullmatch(r'[0-9a-f]*', raw) is not None and
                     len(raw) % 16 == 0 and len(raw) <= 65536, 'BPF instruction encoding invalid')
        host.require(body['license'] is None or type(body['license']) is str and body['license'].isascii() and
                     len(body['license']) <= 63, 'BPF license invalid')
        host.require(body['command'] == 'PROG_LOAD' or raw is None and body['license'] is None,
                     'BPF program supplied for other command')
        internal_detach = (body['grant_id'] is not None and body['command'] == 'PROG_DETACH' and
                           all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
        internal_replacement = (body['grant_id'] is not None and body['command'] in ('PROG_LOAD', 'PROG_ATTACH') and
                                all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
        if internal_detach:
            host.require(attr == {'attr_size': 16, 'attr_hex': struct.pack('<IIII', 0, 0, 6, 0).hex()} and
                         len(body['input_objects']) == 2 and
                         [(item.get('field'), item.get('kind'), item.get('requester_fd')) for item in body['input_objects']
                          if type(item) is dict] == [('target_fd', 'cgroup', None), ('attach_bpf_fd', 'program', None)] and
                         body['returned_fd'] is None, 'BPF internal detach fixed inputs differ')
        for item in body['input_objects']:
            host.require(_shape(item, ('field', 'kind', 'cookie', 'requester_fd')) and type(item['field']) is str and
                         item['kind'] in ('cgroup', 'program', 'map', 'dev_null') and _integer(item['cookie'], 1) and
                         (((internal_detach or internal_replacement) and item['requester_fd'] is None) or
                          (not (internal_detach or internal_replacement) and _integer(item['requester_fd'], high=(1 << 31) - 1))), 'BPF input object invalid')
        if cleanup['policy'] == 'close_borrowed_ref':
            host.require(body['command'] == 'PROG_GET_FD_BY_ID' and body['grant_id'] is not None and
                         ((cleanup['state'] == 'pending' and cleanup['steps'] == []) or
                          (cleanup['state'] in ('settled', 'unknown') and len(cleanup['steps']) == 1 and
                           _shape(cleanup['steps'][0], ('sequence', 'action', 'object_cookie', 'attachment_id', 'guard_sequence', 'result')) and
                           cleanup['steps'][0]['sequence'] == 1 and cleanup['steps'][0]['action'] == 'close_ref' and
                           _integer(cleanup['steps'][0]['object_cookie'], 1) and cleanup['steps'][0]['attachment_id'] is None and
                           cleanup['steps'][0]['guard_sequence'] is None and
                           type(cleanup['steps'][0]['result']) is int and
                           (cleanup['steps'][0]['result'] == 0 if cleanup['state'] == 'settled' else cleanup['steps'][0]['result'] < 0))),
                         'BPF borrowed descriptor cleanup is invalid')
        if body['command'] == 'OBJ_GET_INFO_BY_FD' and body['input_objects'] and body['output'] is not None:
            output = body['output']
            host.require(len(body['input_objects']) == 1 and body['input_objects'][0]['kind'] in ('program', 'map') and
                         _shape(output, ('info_len', 'object_cookie', 'type', 'kernel_id', 'attributes')) and
                         _integer(output['info_len'], 1, 65536) and
                         output['object_cookie'] == body['input_objects'][0]['cookie'] and
                         _integer(output['type'], 1, (1 << 32) - 1) and _integer(output['kernel_id'], 1, (1 << 32) - 1) and
                         type(output['attributes']) is dict and _integer(body['result']) and
                         body['state'] in ('effect_observed', 'response_settled', 'retired'),
                         'BPF held object info is not an actual successful transaction')
        for step in cleanup['steps']:
            host.require(_shape(step, ('sequence', 'action', 'object_cookie', 'attachment_id', 'guard_sequence', 'result')) and
                         _integer(step['sequence'], 1) and step['action'] in ('close_ref', 'detach', 'await_id_removed', 'await_cgroup_release') and
                         all(step[field] is None or _integer(step[field], 1) for field in ('object_cookie', 'attachment_id', 'guard_sequence')) and
                         (step['result'] is None or _integer(step['result'], _I64_MIN, _I64_MAX)), 'BPF cleanup step invalid')
    elif kind == 'checkpoint':
        host.require(_hex(body['nonce'], _HEX32) and _integer(body['begin_watermark']) and
                     _integer(body['end_watermark']) and body['end_watermark'] >= body['begin_watermark'] and
                     _hex(body['tail_before'], nonzero=False), 'BPF checkpoint values invalid')
    elif kind == 'object':
        program = body['class'] == 'program'
        keys = ('name', 'tag', 'load_time', 'created_by_uid', 'translated_insn_count', 'verified_insns', 'attach_btf_id') if program else ('name', 'key_size', 'value_size', 'max_entries', 'map_flags', 'btf_id', 'btf_key_type_id', 'btf_value_type_id', 'map_extra')
        attrs = body['attributes']
        host.require(_integer(body['type'], 1, (1 << 32) - 1) and _shape(attrs, keys) and
                     type(attrs['name']) is str and attrs['name'].isascii() and
                     len(attrs['name']) <= 16 and all(0x20 <= ord(c) <= 0x7e for c in attrs['name']) and
                     all(_integer(value, high=(_U64 if key in ('load_time', 'map_extra') else (1 << 32) - 1))
                         for key, value in attrs.items() if key not in ('name', 'tag')),
                     'BPF object information invalid')
        if program:
            host.require(type(attrs['tag']) is str and re.fullmatch(r'[0-9a-f]{16}', attrs['tag']) is not None and
                         type(body['bytes']) is str and re.fullmatch(r'[0-9a-f]*', body['bytes']) is not None and
                         len(body['bytes']) % 16 == 0 and len(body['bytes']) <= 65536, 'BPF program object invalid')
        else:
            host.require(body['bytes'] is None and (attrs['btf_id'] != 0 or
                         attrs['btf_key_type_id'] == attrs['btf_value_type_id'] == 0),
                         'BPF map instructions or BTF identity invalid')
        source = body['attribute_source']
        host.require(_shape(source, ('kind', 'guard_sequence')) and source['kind'] == 'native_publication' and
                     _integer(source['guard_sequence'], 1) and source['guard_sequence'] == body['publication_seq'],
                     'BPF object publication source invalid')
        host.require((body['state'] == 'live') == (body['removal_seq'] is None), 'BPF object removal state differs')
    elif kind == 'attachment':
        host.require(_integer(body['attach_type'], high=(1 << 32) - 1) and
                     _integer(body['flags'], high=(1 << 32) - 1), 'BPF attachment values invalid')
    elif kind == 'audit':
        _audit_stamp(host, body)
        host.require(body['command'] is None or type(body['command']) is str and body['command'] in
                     ('MAP_CREATE', 'MAP_LOOKUP_ELEM', 'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'MAP_FREEZE',
                      'PROG_LOAD', 'PROG_QUERY', 'PROG_GET_FD_BY_ID', 'OBJ_GET_INFO_BY_FD', 'PROG_ATTACH',
                      'PROG_DETACH', 'LINK_CREATE', 'LINK_DETACH', 'BTF_LOAD'), 'BPF audit command invalid')
        host.require((body['result'] is None or _integer(body['result'], _I64_MIN, _I64_MAX)) and
                     (body['record_type'] == 'SYSCALL' and body['operation'] is None or
                      body['record_type'] == 'BPF' and body['operation'] in ('LOAD', 'UNLOAD')) and
                     (body['context'] != 'syscall' or body['serial'] is not None) and
                     (body['context'] != 'standalone' or body['record_type'] == 'BPF' and
                      body['task_cookie'] is None), 'BPF audit context invalid')
        if body['record_type'] == 'BPF':
            host.require(body['object_cookie'] is not None and body['transaction_id'] is not None and body['result'] is None and
                         ((body['context'] == 'standalone') == (body['task_cookie'] is None)),
                         'BPF audit program identity invalid')
        else:
            host.require(body['context'] == 'syscall' and body['task_cookie'] is not None and
                         body['object_cookie'] is None and body['result'] is not None,
                         'BPF audit syscall identity invalid')
        raw = body['raw']
        if raw is not None:
            host.require(body['record_type'] == 'SYSCALL' and body['context'] == 'syscall' and
                         body['transaction_id'] is None and body['object_cookie'] is None and body['command'] in _COMMAND_IDS and
                         _shape(raw, ('tgid', 'syscall_payload_sha256', 'syscall_occurrence',
                                      'eoe_payload_sha256', 'eoe_occurrence')) and
                         _integer(raw['tgid'], 1, (1 << 32) - 1) and
                         _hex(raw['syscall_payload_sha256']) and _hex(raw['eoe_payload_sha256']) and
                         _integer(raw['syscall_occurrence'], 1) and _integer(raw['eoe_occurrence'], 1),
                         'BPF raw audit provenance invalid')
    elif kind == 'audit_wire':
        _audit_stamp(host, body)
        host.require(type(body['record_type']) is int and body['record_type'] in (1300, 1305, 1306, 1309, 1320, 1334) and
                     _hex(body['payload_sha256']) and _integer(body['occurrence'], 1), 'BPF audit wire invalid')
    elif kind == 'selftest_audit_reorder':
        host.require(body['name'] == 'audit-interleaved' and _hex(body['nonce'], _HEX32) and
                     type(body['received']) is list and len(body['received']) == 4 and
                     type(body['delivery_order']) is list and len(body['delivery_order']) == 4 and
                     all(type(index) is int for index in body['delivery_order']) and
                     sorted(body['delivery_order']) == [1, 2, 3, 4], 'BPF audit reorder witness invalid')
        for wire in body['received']:
            _record_body(host, 'audit_wire', wire)
            host.require(wire['record_type'] in (1300, 1320), 'BPF audit reorder receipt type invalid')
    elif kind == 'audit_cut':
        host.require(all(_integer(body[key], 1) for key in ('principal_id', 'readback_sequence', 'boundary_sequence')) and
                     _integer(body['wire_count']) and _hex(body['boundary_sha256']), 'BPF audit cut invalid')
    elif kind == 'audit_control':
        _audit_stamp(host, body)
        actor = body['actor']
        host.require(_shape(actor, ('task_cookie', 'tgid', 'tid', 'start_ns', 'exe_sha256')) and
                     all(_integer(actor[key], 1) for key in ('task_cookie', 'tgid', 'tid', 'start_ns')) and
                     _hex(actor['exe_sha256']) and body['phase'] in ('early_setup', 'final_rules', 'final_lock', 'unicast_claim') and
                     body['change'] in ('enabled', 'failure', 'rate_limit', 'backlog_limit', 'add_rule', 'audit_pid') and
                     all(value is None or _integer(value, high=(1 << 32) - 1) for value in (body['old_value'], body['new_value'])) and
                     ((body['change'] == 'add_rule' and type(body['rule_list']) is list) or
                      (body['change'] != 'add_rule' and body['rule_list'] is None)) and
                     type(body['result']) is int and body['result'] in (0, 1), 'BPF audit control invalid')
    elif kind == 'audit_readback':
        host.require(_integer(body['reader_cookie'], 1) and _integer(body['request_id'], 1) and
                     _hex(body['nonce'], _HEX32) and body['check'] in ('status', 'configuration') and
                     type(body['queries']) is list and len(body['queries']) == (1 if body['check'] == 'status' else 3),
                     'BPF audit readback invalid')
    elif kind == 'runtime_exec':
        host.require(_hex(body['exe_sha256']), 'BPF runtime executable digest invalid')
    elif kind == 'held_file':
        host.require(body['kind'] == 'dev_null' and body['source'] in ('bootstrap', 'runtime_import') and
                     all(_integer(body[key], 1) for key in ('device', 'rdev')) and
                     all(_integer(body[key], high=(1 << 32) - 1) for key in ('mode', 'uid', 'gid')) and
                     stat.S_ISCHR(body['mode']) and stat.S_IMODE(body['mode']) == 0o666 and
                     body['uid'] == body['gid'] == 0 and os.major(body['rdev']) == 1 and os.minor(body['rdev']) == 3,
                     'BPF held device facts invalid')
        binding = ('invocation_id', 'requester_cookie', 'notification_id', 'requester_fd', 'baseline_cookie')
        host.require((body['source'] == 'bootstrap' and all(body[key] is None for key in binding)) or
                     (body['source'] == 'runtime_import' and all(body[key] is not None for key in binding) and
                      _integer(body['requester_fd'], high=(1 << 31) - 1)), 'BPF held device source binding invalid')
    elif kind == 'grant_object_cleanup':
        host.require(type(body['step']) is int and body['step'] in (1, 2) and
                     _integer(body['result'], _I64_MIN, _I64_MAX) and
                     ((body['step'] == 1 and body['action'] == 'close_ref' and body['guard_sequence'] is None) or
                      (body['step'] == 2 and body['action'] == 'await_id_removed' and body['guard_sequence'] is not None)),
                     'BPF grant object cleanup facts invalid')
    elif kind == 'registry':
        if body['map'] == 'broker_token':
            host.require(body['grant_id'] is None and body['parent_transaction_id'] is None and body['action'] == 'insert' and
                         _shape(body['key'], ('pidfd',)) and _shape(body['value'], ('cookie',)) and
                         _integer(body['key']['pidfd'], high=(1 << 31) - 1) and _integer(body['value']['cookie'], 1),
                         'BPF broker token registry invalid')
        elif body['map'] == 'guard_liveness':
            host.require(body['grant_id'] is None and body['parent_transaction_id'] is None and body['action'] == 'update' and
                         _shape(body['key'], ('slot',)) and _shape(body['value'], ('phase',)) and
                         body['key']['slot'] == 0 and body['value']['phase'] == 1,
                         'BPF guard liveness registry invalid')
        else:
            key_fields = ('root_kn_id', 'kn_id') if body['map'] == 'realm_targets' else ('root_kn_id', 'kn_id', 'tgid', 'leader_start_ns')
            value_fields = ('enrollment', 'grant_id', 'phase', 'retirement') if body['map'] == 'realm_targets' else ('enrollment', 'grant_id', 'invocation_id', 'exe_dev', 'exe_ino')
            host.require(_integer(body['grant_id'], 1) and _shape(body['key'], key_fields) and
                         all(_integer(value, 1) for value in body['key'].values()) and
                         ((body['action'] == 'delete' and body['value'] is None) or
                          (body['action'] != 'delete' and _shape(body['value'], value_fields))),
                         'BPF registry key or value invalid')
            if body['value'] is not None:
                host.require(all(_integer(value, 0 if key == 'invocation_id' else 1) for key, value in body['value'].items()) and
                             body['value']['grant_id'] == body['grant_id'] and
                             (body['map'] != 'realm_targets' or
                              body['value']['phase'] in (1, 2, 3, 4) and body['value']['retirement'] in (1, 2)),
                             'BPF registry ownership or state invalid')
    elif kind == 'guard':
        guard = body['record']
        host.require(_shape(guard, 'sequence kind flags object_cookie kernel_id root_cookie cgroup_id task_cookie task_start_ns audit_serial operation result parent_cookie detail object_snapshot'.split()) and
                     _integer(guard['sequence'], 1) and _integer(guard['kind'], 1, 16) and _integer(guard['flags'], high=7) and
                     _integer(guard['result'], -(1 << 31), (1 << 31) - 1) and
                     all(_integer(guard[field]) for field in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'task_cookie', 'task_start_ns', 'audit_serial', 'operation', 'parent_cookie')),
                     'BPF guard event invalid')
        snapshot = guard['object_snapshot']
        if guard['kind'] in (1, 3):
            host.require(_shape(snapshot, ('version', 'type', 'attributes')) and type(snapshot['version']) is int and
                         snapshot['version'] == 1 and _integer(snapshot['type'], 1, (1 << 32) - 1),
                         'BPF guard publication snapshot invalid')
            attrs = snapshot['attributes']
            keys = (('name', 'tag', 'load_time', 'created_by_uid', 'translated_insn_count', 'verified_insns', 'attach_btf_id')
                    if guard['kind'] == 1 else
                    ('name', 'key_size', 'value_size', 'max_entries', 'map_flags', 'btf_id', 'btf_key_type_id', 'btf_value_type_id', 'map_extra'))
            host.require(_shape(attrs, keys) and type(attrs['name']) is str and attrs['name'].isascii() and
                         len(attrs['name']) <= 16 and all(0x20 <= ord(c) <= 0x7e for c in attrs['name']) and
                         all(_integer(v, high=(_U64 if k in ('load_time', 'map_extra') else (1 << 32) - 1))
                             for k, v in attrs.items() if k not in ('name', 'tag')) and
                         (guard['kind'] != 1 or type(attrs['tag']) is str and re.fullmatch(r'[0-9a-f]{16}', attrs['tag']) is not None),
                         'BPF guard publication attributes invalid')
            if guard['kind'] == 3:
                host.require(attrs['btf_id'] != 0 or attrs['btf_key_type_id'] == attrs['btf_value_type_id'] == 0,
                             'BPF guard map without BTF has BTF type IDs')
        else:
            host.require(snapshot is None, 'BPF non-publication guard has snapshot')
        if guard['kind'] <= 7:
            host.require(guard['detail'] is None and not (guard['flags'] & 1) and
                         (guard['kind'] != 7 or guard['audit_serial'] == 0), 'BPF guard event detail invalid')
        elif guard['kind'] == 8:
            detail = guard['detail']
            host.require(_shape(detail, ('seconds', 'nanoseconds')) and
                         _integer(detail['seconds'], high=_I64_MAX) and
                         _integer(detail['nanoseconds'], high=999999999) and
                         _integer(guard['audit_serial'], 1, (1 << 32) - 1), 'BPF guard audit stamp invalid')
        elif guard['kind'] in (9, 10, 11, 12, 15):
            detail = guard['detail']
            host.require(guard['flags'] == 0 and all(guard[field] == 0 for field in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'audit_serial', 'operation')) and
                         (guard['kind'] == 15 or guard['result'] == 0) and
                         _shape(detail, ('tgid', 'tid', 'exe_dev', 'exe_ino', 'exec_generation', 'exit_code')) and
                         all(_integer(detail[field]) for field in ('tgid', 'tid', 'exe_dev', 'exe_ino', 'exec_generation')) and
                         _integer(detail['exit_code'], -(1 << 31), (1 << 31) - 1) and
                         (guard['kind'] in (11, 15) or detail['exit_code'] == 0) and
                         (guard['kind'] != 15 or detail['exit_code'] == guard['result']), 'BPF task guard detail invalid')
        elif guard['kind'] == 13:
            detail = guard['detail']
            host.require(guard['flags'] == 0 and all(guard[field] == 0 for field in
                         ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'parent_cookie')) and
                         _integer(guard['task_cookie'], 1) and _integer(guard['task_start_ns'], 1) and
                         _integer(guard['audit_serial'], 1, (1 << 32) - 1) and
                         _integer(guard['operation'], high=(1 << 32) - 1) and
                         _shape(detail, ('tgid', 'tid', 'seconds', 'nanoseconds', 'exe_dev', 'exe_ino')) and
                         all(_integer(detail[field], 1, (1 << 32) - 1) for field in ('tgid', 'tid')) and
                         _integer(detail['seconds'], high=_I64_MAX) and _integer(detail['nanoseconds'], high=999999999) and
                         _integer(detail['exe_dev']) and _integer(detail['exe_ino'], 1),
                         'BPF raw syscall guard detail invalid')
        elif guard['kind'] == 14:
            host.require(guard['flags'] == 0 and guard['detail'] is None and guard['operation'] in (0, 2) and
                         all(guard[field] == 0 for field in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'audit_serial', 'result', 'parent_cookie')) and
                         _integer(guard['task_cookie'], 1) and _integer(guard['task_start_ns'], 1),
                         'BPF authority transition guard detail invalid')
        elif guard['kind'] == 16:
            detail = guard['detail']
            host.require(guard['flags'] == 0 and all(guard[k] == 0 for k in ('root_cookie', 'cgroup_id', 'audit_serial', 'parent_cookie')) and
                         _integer(guard['object_cookie'], 1) and _integer(guard['kernel_id'], 1, (1 << 31) - 2) and
                         _integer(guard['task_cookie'], 1) and _integer(guard['task_start_ns'], 1) and
                         _shape(detail, ('first_removal_sequence', 'transaction_id', 'saved_cursor', 'requested_id', 'allocator_result', 'cursor_after')) and
                         all(_integer(detail[k], 1) for k in ('first_removal_sequence', 'transaction_id')) and
                         all(_integer(detail[k], high=(1 << 32) - 1) for k in ('saved_cursor', 'requested_id', 'cursor_after')) and
                         _integer(detail['allocator_result'], -(1 << 31), (1 << 31) - 1) and detail['requested_id'] == guard['kernel_id'] and
                         guard['operation'] in (1, 2, 3) and
                         (guard['operation'] != 1 or (detail['saved_cursor'], detail['allocator_result'], detail['cursor_after'], guard['result']) == (0, 0, 0, 0)) and
                         (guard['operation'] != 2 or detail['allocator_result'] == 0 and detail['cursor_after'] == guard['kernel_id'] and guard['result'] == 0) and
                         (guard['operation'] != 3 or detail['cursor_after'] == detail['saved_cursor'] and guard['result'] == detail['allocator_result']),
                         'BPF selftest ID cursor detail invalid')
    elif kind == 'module_instance':
        host.require(_hex(body['module_sha256']) and _hex(body['observer_bindings_sha256']) and
                     _shape(body['load_file'], ('dev', 'ino', 'size')) and
                     _integer(body['load_file']['dev']) and _integer(body['load_file']['ino'], 1) and
                     _integer(body['load_file']['size'], 1, 128 * 1024 * 1024),
                     'BPF module instance record invalid')
    elif kind == 'probe_actor':
        host.require(type(body['name']) is str and body['name'] and _hex(body['exe_sha256']),
                     'BPF probe actor record invalid')
    elif kind == 'probe_import':
        host.require(_integer(body['slot'], high=(1 << 32) - 1) and body['fixture'] in (None, 'a', 'b') and
                     _integer(body['guard_begin']) and _integer(body['guard_end'], body['guard_begin']) and
                     body['result'] in ('imported', 'mismatch', 'ebadf', 'esrch', 'denied'), 'BPF probe import record invalid')
    elif kind == 'selftest_runtime_stage':
        host.require(_hex(body['nonce'], _HEX32) and _integer(body['grant_id'], 1) and
                     body['mode'] in ('nested-buffer-race', 'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel', 'wrong-program-byte', 'wrong-flags', 'wrong-target', 'extra-fd-delays-removal') and
                     body['phase'] in ('armed', 'captured', 'mutated', 'committed', 'effect_observed', 'replied', 'cancelled', 'settled', 'injected'),
                     'BPF selftest runtime stage record invalid')
        if body['mode'] == 'extra-fd-delays-removal':
            mutation = body['mutation']
            host.require(body['phase'] == 'injected' and _shape(mutation, ('program_cookie', 'listener_id', 'installed_fd')) and
                         _integer(mutation['program_cookie'], 1) and _integer(mutation['listener_id'], 1) and
                         _integer(mutation['installed_fd'], high=(1 << 31) - 1) and
                         _integer(body['notifying_cookie'], 1) and _integer(body['notification_id'], 1) and
                         body['transaction_id'] is None and body['signal'] is None,
                         'BPF extra descriptor transfer evidence invalid')
    elif kind == 'selftest_case':
        host.require(type(body['name']) is str and body['name'] in _BASELINE_OBSERVATION_NAMES and
                     _hex(body['nonce'], _HEX32) and body['phase'] in ('started', 'observed', 'settled') and
                     all(body[k] is None or _integer(body[k], 1) for k in ('actor_cookie', 'invocation_id')) and
                     all(type(body[k]) is list and all(_integer(v, 1) for v in body[k]) for k in
                         ('transaction_ids', 'guard_sequences', 'reference_cookies')) and
                     (body['request_sha256'] is None or _hex(body['request_sha256'])) and
                     (body['result'] is None or _integer(body['result'], _I64_MIN, _I64_MAX)) and
                     body['error_code'] in (None, 'BPF_AUTH_DENIED', 'BPF_AUTH_UNAVAILABLE', 'BPF_AUTH_PENDING', 'BPF_AUTH_INCOMPATIBLE'),
                     'BPF selftest case record invalid')
    elif kind == 'selftest_guard_status':
        status = body['status']
        _terminal_status(host, status)
        host.require(body['name'] in ('seal-unload-denied', 'seal-detach-denied') and
                     _hex(body['nonce'], _HEX32) and body['phase'] in ('before', 'after') and
                     status['state'] == 'sealed' and status['authority_phase'] == 1 and
                     all(_integer(v, 1, (1 << 32) - 1) for v in status['link_ids']) and
                     len(set(status['link_ids'])) == 3 and status['link_attached'] == [True, True, True],
                     'BPF selftest guard status invalid')
    elif kind == 'control_rejection':
        host.require(_integer(body['request_id'], 1) and
                     ((body['principal_id'] is None and body['nonce'] is None) if body['operation'] == 'BIND_RUNTIME'
                      else (_integer(body['principal_id'], 1) and _hex(body['nonce'], _HEX32))) and
                     type(body['operation']) is str and bool(body['operation']) and
                     type(body['body']) is dict and _hex(body['request_sha256']) and
                     body['status'] in ('OK', 'BPF_AUTH_DENIED', 'BPF_AUTH_UNAVAILABLE', 'BPF_AUTH_PENDING', 'BPF_AUTH_INCOMPATIBLE') and
                     ((body['status'] == 'OK' and body['code'] is None) or body['code'] == body['status'] != 'OK') and
                     _integer(body['guard_before']) and _integer(body['guard_after'], body['guard_before']),
                     'BPF control rejection record invalid')
    elif kind == 'exec_admission':
        host.require(body['observation'] in ('negative_pg_dump', 'app_rollback_dump', 'app_rollback_restore', 'pg_controldata'),
                     'BPF exec admission record invalid')
    elif kind == 'endpoint_transition':
        host.require(type(body['recovery_id']) is str and body['recovery_id'] and _hex(body['scope_sha256']),
                     'BPF endpoint transition record invalid')
    elif kind == 'notification_denial':
        host.require(all(_integer(body[key], 1) for key in ('invocation_id', 'requester_cookie', 'notification_id')) and
                     _integer(body['command'], high=(1 << 32) - 1) and type(body['result']) is int and body['result'] == -1 and
                     _integer(body['guard_before']) and _integer(body['guard_after'], body['guard_before']),
                     'BPF notification denial facts invalid')
    elif kind == 'notification_response':
        host.require(body['operation'] in ('SEND', 'ADDFD_SEND') and type(body['ioctl_errno']) is int and
                     body['ioctl_errno'] in (0, 2) and _integer(body['guard_before']) and
                     _integer(body['guard_after'], body['guard_before']), 'BPF notification response facts invalid')
        if body['operation'] == 'SEND':
            response = body['response']
            host.require(_shape(response, ('value', 'error', 'flags')) and _integer(response['value'], _I64_MIN, _I64_MAX) and
                         _integer(response['error'], -4095, 0) and type(response['flags']) is int and response['flags'] == 0 and
                         (response['error'] == 0 or response['value'] == 0) and body['returned_fd'] is None,
                         'BPF attempted notification SEND differs')
        else:
            host.require(body['response'] is None and
                         ((body['ioctl_errno'] == 0 and _integer(body['returned_fd'], high=(1 << 31) - 1)) or
                          (body['ioctl_errno'] == 2 and body['returned_fd'] is None)),
                         'BPF attempted notification ADDFD_SEND differs')
    elif kind == 'notification_cancel':
        host.require(all(_integer(body[key], 1) for key in ('invocation_id', 'requester_cookie', 'notification_id')) and
                     _integer(body['command'], high=(1 << 32) - 1) and type(body['validation_errno']) is int and
                     body['validation_errno'] == 2 and _integer(body['guard_before']) and
                     _integer(body['guard_after'], body['guard_before']), 'BPF notification cancellation facts invalid')
    elif kind == 'selftest_fd_stage':
        host.require(_hex(body['nonce'], _HEX32) and _integer(body['grant_id'], 1) and
                     body['class'] in ('map', 'program') and body['phase'] in ('armed', 'entered', 'restored'),
                     'BPF FD selftest identity invalid')
        fields = ('transaction_id', 'saved_soft', 'hard_limit', 'test_soft', 'original_fd_count', 'identity_fd_count', 'owned_fd_count', 'errno')
        if body['phase'] == 'armed':
            host.require(all(body[key] is None for key in fields), 'BPF armed FD selftest has resource facts')
        else:
            host.require(_integer(body['transaction_id'], 1) and _integer(body['saved_soft'], 256) and
                         _integer(body['hard_limit'], body['saved_soft']) and type(body['test_soft']) is int and
                         body['test_soft'] == 256 and _integer(body['original_fd_count'], 1, 128) and
                         _integer(body['identity_fd_count'], 1, 128) and body['identity_fd_count'] == body['original_fd_count'] and
                         _integer(body['owned_fd_count'], 0, 256) and
                         body['original_fd_count'] + body['identity_fd_count'] + body['owned_fd_count'] <= 256 and
                         ((body['phase'] == 'entered' and body['errno'] is None) or
                          (body['phase'] == 'restored' and type(body['errno']) is int and body['errno'] == 24)),
                         'BPF FD selftest resource facts invalid')


def _chain(host, genesis, suffix):
    host.require(_shape(genesis, _BOOTSTRAP) and _shape(suffix, _SUFFIX), 'BPF evidence envelope differs')
    host.require(type(genesis['version']) is int and genesis['version'] == 2 and
                 type(suffix['version']) is int and suffix['version'] == 2 and
                 type(genesis['boot_id']) is str and _UUID.fullmatch(genesis['boot_id']) is not None and
                 _hex(genesis['epoch'], _HEX32) and suffix['boot_id'] == genesis['boot_id'] and
                 suffix['epoch'] == genesis['epoch'] and type(genesis['records']) is list and
                 type(suffix['records']) is list and genesis['records'] and
                 _integer(genesis['broker_principal'], 1), 'BPF boot identity or records invalid')
    records = genesis['records'] + suffix['records']
    _json(host, genesis)
    _json(host, suffix)
    host.require(len(_canonical(genesis)) + len(_canonical(suffix)) <= _MAX_EVIDENCE_BYTES,
                 'BPF evidence exceeds result limit')
    previous = _ZERO
    hashes = [_ZERO]
    guard_sequence = 0
    charged = 0
    for index, event in enumerate(records, 1):
        host.require(_shape(event, _ENVELOPE) and type(event['version']) is int and event['version'] == 1 and
                     _integer(event['sequence'], 1) and event['sequence'] == index and
                     event['boot_id'] == genesis['boot_id'] and event['epoch'] == genesis['epoch'] and
                     event['previous'] == previous and type(event['kind']) is str and event['kind'] in _RECORD_KEYS,
                     'BPF transcript chain or record kind differs')
        _record_body(host, event['kind'], event['body'])
        raw = _canonical(event)
        charged += len(raw) + 1
        host.require(charged <= _MAX_TRANSCRIPT_BYTES, 'BPF transcript exceeds byte limit')
        host.require(0 < len(raw) <= 16384, 'BPF transcript record exceeds limit')
        previous = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + raw).hexdigest()
        hashes.append(previous)
        if event['kind'] == 'guard':
            guard_sequence += 1
            host.require(event['body']['record']['sequence'] == guard_sequence, 'BPF guard event sequence gap')
    prefix = len(genesis['records'])
    host.require(genesis['bootstrap_tail'] == hashes[prefix] == suffix['genesis_tail'] and
                 _integer(suffix['final_watermark']) and suffix['final_watermark'] == len(records) and
                 suffix['final_tail'] == previous, 'BPF transcript tails or watermark differ')
    status = suffix['guard_status']
    host.require(_shape(status, ('state', 'sticky', 'sequence', 'lost', 'probe_missed')) and
                 status['state'] == 'sealed' and all(_integer(status[field]) for field in ('sticky', 'sequence', 'lost', 'probe_missed')) and
                 status['sticky'] == status['lost'] == status['probe_missed'] == 0 and
                 status['sequence'] <= guard_sequence, 'BPF guard status is not a healthy recorded prefix')
    return records, hashes


def _partial_chain(host, genesis, partial):
    host.require(_shape(genesis, _BOOTSTRAP) and type(genesis['version']) is int and genesis['version'] == 2 and
                 type(genesis['boot_id']) is str and _UUID.fullmatch(genesis['boot_id']) is not None and
                 _hex(genesis['epoch'], _HEX32) and type(genesis['records']) is list and genesis['records'] and
                 _shape(partial, ('records', 'watermark', 'tail_sha256')) and type(partial['records']) is list and
                 partial['watermark'] == len(partial['records']) and _hex(partial['tail_sha256'], nonzero=False) and
                 partial['records'][:len(genesis['records'])] == genesis['records'],
                 'BPF preserved healthy transcript prefix missing or changed')
    _json(host, partial)
    host.require(len(_canonical(partial)) <= _MAX_EVIDENCE_BYTES, 'BPF partial transcript exceeds result limit')
    previous, hashes, guard_sequence = _ZERO, [_ZERO], 0
    charged = 0
    for index, event in enumerate(partial['records'], 1):
        host.require(_shape(event, _ENVELOPE) and type(event['version']) is int and event['version'] == 1 and
                     type(event['sequence']) is int and event['sequence'] == index and event['previous'] == previous and
                     event['boot_id'] == genesis['boot_id'] and event['epoch'] == genesis['epoch'] and
                     type(event['kind']) is str and event['kind'] in _RECORD_KEYS, 'BPF partial transcript chain differs')
        _record_body(host, event['kind'], event['body'])
        raw = _canonical(event)
        charged += len(raw) + 1
        host.require(charged <= _MAX_TRANSCRIPT_BYTES, 'BPF partial transcript exceeds byte limit')
        host.require(len(raw) <= 16384, 'BPF partial transcript record exceeds limit')
        previous = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + raw).hexdigest()
        hashes.append(previous)
        if event['kind'] == 'guard':
            guard_sequence += 1
            host.require(event['body']['record']['sequence'] == guard_sequence, 'BPF healthy prefix has a guard gap')
    host.require(hashes[len(genesis['records'])] == genesis['bootstrap_tail'] and previous == partial['tail_sha256'],
                 'BPF partial transcript tail differs')
    return partial['records'], hashes


def _build_binding(host, genesis, build):
    host.require(type(build) is dict and type(build.get('boot_inputs')) is dict and
                 type(build.get('bpf_authority')) is dict, 'selected BPF build inputs missing')
    inputs, authority = build['boot_inputs'], build['bpf_authority']
    manifest = authority.get('bootstrap_manifest')
    native = authority.get('native_artifacts')
    host.require(type(manifest) is dict and type(native) is dict and
                 _hash(inputs) == genesis['boot_inputs_sha256'] and
                 _hash(manifest) == inputs.get('bootstrap_manifest_sha256') == genesis['bootstrap_manifest_sha256'],
                 'BPF boot or bootstrap manifest commitment differs')
    executables = manifest.get('package_executables')
    host.require(type(executables) is dict and set(executables) == {'python', 'systemd', 'runc', 'dockerd', 'containerd', 'shim'} and
                 all(_shape(item, ('package', 'version', 'path', 'sha256')) and
                     all(type(item[key]) is str and item[key] for key in ('package', 'version', 'path')) and
                     item['path'].startswith('/') and '..' not in item['path'].split('/') and _hex(item['sha256'])
                     for item in executables.values()) and executables['systemd']['package'] == 'systemd' and
                 executables['systemd']['path'] == '/usr/lib/systemd/systemd', 'BPF executable manifest schema differs')
    host.require(_hex(genesis.get('observer_bindings_sha256')) and
                 genesis['observer_bindings_sha256'] == inputs.get('observer_bindings_sha256') ==
                 manifest.get('observer_bindings_sha256'), 'BPF observer binding commitment differs')
    for field in ('broker_sha256', 'guard_sha256', 'policy_sha256', 'generator_sha256'):
        host.require(_hex(manifest.get(field)) and genesis[field] == manifest[field], 'BPF selected %s differs' % field)
    host.require(manifest['guard_sha256'] == native.get('module_sha256') and
                 manifest['policy_sha256'] == native.get('policy_sha256') and
                 manifest['broker_sha256'] == build.get('binaries', {}).get('g2-bpf-authority') and
                 manifest['generator_sha256'] == build.get('bpf_authority_sources', {}).get('policy.go') and
                 manifest.get('kernel_sha256') == inputs.get('kernel_sha256') and
                 manifest.get('original_initrd_sha256') == inputs.get('original_initrd_sha256'),
                 'BPF build artifacts do not match selected source and boot')
    links = genesis['links']
    host.require(type(links) is list and len(links) == 3 and
                 all(_shape(item, ('hook', 'link_id', 'program_cookie')) and
                     _integer(item['link_id'], 1, (1 << 32) - 1) and _integer(item['program_cookie'], 1) for item in links) and
                 [item['hook'] for item in links] == ['bpf', 'inode_rmdir', 'inode_mkdir'] and
                 len({item['link_id'] for item in links}) == len({item['program_cookie'] for item in links}) == 3,
                 'BPF bootstrap link identities invalid')


def _policy_elf(host, build):
    path = Path(build['_attempt_dir']) / 'native/g2-policy.o'
    host.check_path(path, False)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        host.require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and
                     0 < before.st_size <= 32 * 1024 * 1024, 'BPF policy ELF identity invalid')
        chunks = []
        size = 0
        while size < before.st_size:
            data = os.read(fd, min(1024 * 1024, before.st_size - size))
            host.require(bool(data), 'BPF policy ELF read was short')
            chunks.append(data)
            size += len(data)
        after, named = os.fstat(fd), os.stat(path, follow_symlinks=False)
        identity = lambda st: (st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
        raw = b''.join(chunks)
        host.require(identity(before) == identity(after) == identity(named) and
                     hashlib.sha256(raw).hexdigest() == build['bpf_authority']['bootstrap_manifest']['policy_sha256'],
                     'BPF selected policy ELF changed')
        return raw
    finally:
        os.close(fd)


def _independent_layout(host, raw, actual):
    import source_bpf_core
    btf = source_bpf_core._BTF(raw)
    def member(owner, name):
        found = btf.member(owner, name)
        host.require(len(found) == 1 and found[0][1] % 8 == 0 and found[0][2] == 0,
                     'BPF independent layout member invalid')
        return found[0][0], found[0][1] // 8
    names = ('bpf_prog', 'bpf_prog_aux', 'bpf_map', 'user_struct', 'btf')
    types = {name: btf.unique(4, name) for name in names}
    types['kuid'] = btf.unique(8, 'kuid_t')
    layout = {name + '_struct_size': btf.size(ident) for name, ident in types.items()}
    fields = {
        'bpf_prog': ('len', 'type', 'aux', 'tag'),
        'bpf_prog_aux': ('load_time', 'verified_insns', 'attach_btf_id', 'user', 'offload', 'offload_requested', 'name'),
        'bpf_map': ('map_type', 'ops', 'btf', 'name', 'key_size', 'value_size', 'max_entries',
                    'map_flags', 'map_extra', 'btf_key_type_id', 'btf_value_type_id'),
        'user_struct': ('uid',), 'kuid': ('val',), 'btf': ('id',),
    }
    pointers = {('bpf_prog', 'aux'): 'bpf_prog_aux', ('bpf_prog_aux', 'user'): 'user_struct',
                ('bpf_prog_aux', 'offload'): 'bpf_prog_offload', ('bpf_map', 'ops'): 'bpf_map_ops',
                ('bpf_map', 'btf'): 'btf'}
    arrays = {('bpf_prog', 'tag'): 8, ('bpf_prog_aux', 'name'): 16, ('bpf_map', 'name'): 16}
    for owner, members in fields.items():
        for field in members:
            ident, offset = member(types[owner], field)
            _, resolved = btf.resolve(ident)
            key = owner + '_' + field
            layout[key + '_offset'] = offset
            layout[key + '_size'] = btf.size(ident)
            if (owner, field) in pointers:
                host.require(resolved['kind'] == 2 and btf.size(ident) == 8 and
                             btf.resolve(resolved['value'])[0] == btf.unique(4, pointers[(owner, field)]),
                             'BPF independent pointer target differs')
            elif (owner, field) in arrays:
                host.require(resolved['kind'] == 3, 'BPF independent array type differs')
                element, index, count = struct.unpack('<III', resolved['data'])
                _, element_type = btf.resolve(element)
                host.require(count == arrays[(owner, field)] and element_type['kind'] == 1 and
                             element_type['value'] == 1 and btf.size(index) == 4,
                             'BPF independent array width differs')
            elif (owner, field) == ('user_struct', 'uid'):
                host.require(btf.resolve(ident)[0] == btf.resolve(types['kuid'])[0], 'BPF creator UID type differs')
            elif (owner, field) == ('bpf_prog_aux', 'offload_requested'):
                host.require(resolved['kind'] == 1 and resolved['name'] == '_Bool' and resolved['value'] == 1 and
                             struct.unpack('<I', resolved['data'])[0] == 0x04000008,
                             'BPF independent offload flag differs')
            elif field in ('type', 'map_type'):
                host.require(resolved['kind'] == 6 and resolved['value'] == 4 and
                             resolved['name'] == ('bpf_prog_type' if owner == 'bpf_prog' else 'bpf_map_type'),
                             'BPF independent object type differs')
            else:
                width = 8 if field in ('load_time', 'map_extra') else 4
                host.require(resolved['kind'] == 1 and resolved['value'] == width and
                             struct.unpack('<I', resolved['data'])[0] == width * 8,
                             'BPF independent scalar differs')
    idr, buffer, stamp, context = (btf.unique(4, name) for name in ('idr', 'audit_buffer', 'audit_stamp', 'audit_context'))
    ctx_type, ctx_offset = member(buffer, 'ctx')
    _, pointer = btf.resolve(ctx_type)
    stamp_type, stamp_offset = member(buffer, 'stamp')
    time_type, time_offset = member(stamp, 'ctime')
    sec_type, sec = member(time_type, 'tv_sec')
    nsec_type, nsec = member(time_type, 'tv_nsec')
    serial_type, serial = member(stamp, 'serial')
    class_type, class_offset = member(context, 'context')
    context_stamp, context_stamp_offset = member(context, 'stamp')
    major_type, major_offset = member(context, 'major')
    argv_type, argv_offset = member(context, 'argv')
    retcode_type, retcode_offset = member(context, 'return_code')
    retvalid_type, retvalid_offset = member(context, 'return_valid')
    arch_type, arch_offset = member(context, 'arch')
    _, argv_resolved = btf.resolve(argv_type)
    host.require(argv_resolved['kind'] == 3, 'BPF independent audit argv type differs')
    argv_elem, argv_index, argv_count = struct.unpack('<III', argv_resolved['data'])
    _, argv_elem_resolved = btf.resolve(argv_elem)
    host.require(argv_count == 4 and argv_elem_resolved['kind'] == 1 and argv_elem_resolved['value'] == 8 and
                 struct.unpack('<I', argv_elem_resolved['data'])[0] == 64 and btf.size(argv_index) == 4,
                 'BPF independent audit argv layout differs')
    host.require(pointer['kind'] == 2 and btf.resolve(pointer['value'])[0] == context and
                 btf.resolve(stamp_type)[0] == stamp and btf.resolve(context_stamp)[0] == stamp,
                 'BPF independent audit linkage differs')
    for ident, width, signed in ((sec_type, 8, True), (nsec_type, 8, True), (serial_type, 4, False),
                                 (major_type, 4, True), (retcode_type, 8, True),
                                 (retvalid_type, 4, True), (arch_type, 4, True)):
        _, scalar = btf.resolve(ident)
        encoding = struct.unpack('<I', scalar['data'])[0]
        host.require(scalar['kind'] == 1 and scalar['value'] == width and
                     bool((encoding >> 24) & 1) == signed and encoding & 0xffff == width * 8,
                     'BPF independent audit scalar differs')
    _, enum = btf.resolve(class_type)
    host.require(enum['kind'] == 6 and enum['value'] == 4, 'BPF independent audit enum differs')
    values = {}
    for pos in range(0, len(enum['data']), 8):
        name, value = struct.unpack_from('<II', enum['data'], pos)
        name = source_bpf_core._string(btf.strings, name)
        host.require(name not in values, 'BPF independent audit enum duplicated')
        values[name] = value
    layout.update({'idr_size': btf.size(idr), 'audit_buffer_size': btf.size(buffer),
                   'audit_buffer_ctx_offset': ctx_offset, 'audit_buffer_stamp_offset': stamp_offset,
                   'audit_stamp_size': btf.size(stamp), 'stamp_seconds_offset': time_offset + sec,
                   'stamp_nanoseconds_offset': time_offset + nsec, 'stamp_serial_offset': serial,
                   'stamp_serial_size': btf.size(serial_type), 'audit_context_size': btf.size(context),
                   'context_class_offset': class_offset, 'context_stamp_offset': context_stamp_offset,
                   'context_major_offset': major_offset, 'context_major_size': 4,
                   'context_argv_offset': argv_offset, 'context_argv_size': 32,
                   'context_return_code_offset': retcode_offset, 'context_return_code_size': 8,
                   'context_return_valid_offset': retvalid_offset, 'context_return_valid_size': 4,
                   'context_arch_offset': arch_offset, 'context_arch_size': 4,
                   'context_unused_value': values['AUDIT_CTX_UNUSED'], 'context_syscall_value': values['AUDIT_CTX_SYSCALL'],
                   'context_uring_value': values['AUDIT_CTX_URING']})
    host.require(len(layout) == 81 and actual == layout, 'BPF independent kernel layout differs')


def _original_package_kernel(host, boot_image, boot_identity, directory, inventories, kernel, expected_sha256):
    import source_boot

    env = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}
    host.require(directory in ('/', '/boot'), 'BPF original boot directory invalid')
    kernel_name = 'vmlinuz-' + source_boot.RELEASE
    kernel_path = directory.rstrip('/') + '/' + kernel_name
    kernel_entry = source_boot._listing(boot_image, [directory], env)[directory].get(kernel_name)
    host.require('/boot/' + kernel_name in inventories['linux-image-6.8.0-147-generic'] and
                 kernel_entry is not None and stat.S_ISREG(kernel_entry['mode']) and
                 0 < kernel_entry['size'] <= 1024 ** 3,
                 'BPF original kernel package or partition entry differs')
    boot_fd = source_boot._open_regular(boot_image)
    try:
        host.require(source_boot._identity(os.fstat(boot_fd)) == boot_identity,
                     'BPF verified boot partition changed before kernel read')
        original_kernel, stderr = source_boot._run(
            ['/usr/bin/debugfs', '-R', 'cat ' + kernel_path, '/proc/self/fd/%d' % boot_fd],
            env=env, pass_fds=(boot_fd,), maximum=1024 ** 3)
        host.require(re.fullmatch(rb'debugfs [^\n]+\n', stderr) is not None and
                     source_boot._identity(os.fstat(boot_fd)) == boot_identity ==
                     source_boot._identity(os.stat(boot_image, follow_symlinks=False)),
                     'BPF verified boot partition changed during kernel read')
    finally:
        os.close(boot_fd)
    host.require(len(original_kernel) == kernel_entry['size'] and original_kernel == kernel and
                 hashlib.sha256(original_kernel).hexdigest() == expected_sha256,
                 'BPF original package kernel differs from retained input')
    return original_kernel


def _selected_observer(host, build):
    """Rebuild the binding from the selected image's package and header inventory."""
    import source_boot
    import source_bpf_build
    import source_kernel_observer
    import source_vm

    attempt = Path(build['_attempt_dir'])
    meta = build['bpf_authority']['boot_derivation']
    root = attempt / 'boot-inputs'
    env = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}
    root_part, boot_part = meta['root_partition'], meta['boot_partition']
    host.require(_integer(root_part['index'], 1) and _integer(boot_part['index'], 1),
                 'BPF selected partition identity invalid')
    root_image = root / ('partition-%d.raw' % root_part['index'])
    boot_image = root / ('partition-%d.raw' % boot_part['index'])
    for path in (root_image, boot_image):
        host.check_path(path, False)
    raw_path = root / 'base.raw'
    raw_fd = source_boot._open_regular(raw_path)
    try:
        before = source_boot._identity(os.fstat(raw_fd))
        receipt_path = host.RUN / 'prepare.json'
        host.require(host.digest(receipt_path)[0] == build['prepare_sha256'], 'BPF prepare receipt differs')
        receipt = host.strict_json(receipt_path.read_bytes())
        original = Path(receipt['assets']['ubuntu:ubuntu-24.04-server-cloudimg-amd64.img'])
        original_fd = source_boot._open_regular(original)
        try:
            tool_path = host.RUN / 'build/qemu/qemu-img'
            tool_fd = source_boot._open_regular(tool_path)
            try:
                image_before = source_boot._identity(os.fstat(original_fd))
                tool_before = source_boot._identity(os.fstat(tool_fd))
                host.require(build['boot_inputs']['base_sha256'] == source_boot.BASE_SHA256 and
                             source_boot._hash_fd(original_fd) == source_boot.BASE_SHA256 and
                             source_boot._hash_fd(tool_fd) == source_boot.QEMU_SHA256,
                             'BPF original base image or pinned QEMU tool differs')
                info_raw, _ = source_boot._run(['/proc/self/fd/%d' % tool_fd, 'info', '--output=json',
                                                '/proc/self/fd/%d' % original_fd], env=env,
                                               pass_fds=(tool_fd, original_fd), timeout=60, maximum=65536)
                info = host.strict_json(info_raw)
                host.require(type(info) is dict and info.get('format') == 'qcow2' and
                             'backing-filename' not in info and type(info.get('virtual-size')) is int and
                             info['virtual-size'] == before[6], 'BPF retained base image geometry differs')
                source_boot._run(['/proc/self/fd/%d' % tool_fd, 'compare', '-f', 'qcow2', '-F', 'raw',
                                  '/proc/self/fd/%d' % original_fd, '/proc/self/fd/%d' % raw_fd],
                                 env=env, pass_fds=(tool_fd, original_fd, raw_fd), timeout=900)
                host.require(source_boot._identity(os.fstat(original_fd)) == image_before ==
                             source_boot._identity(os.stat(original, follow_symlinks=False)) and
                             source_boot._identity(os.fstat(tool_fd)) == tool_before ==
                             source_boot._identity(os.stat(tool_path, follow_symlinks=False)) and
                             source_boot._hash_fd(original_fd) == source_boot.BASE_SHA256,
                             'BPF base image or QEMU tool changed during comparison')
            finally:
                os.close(tool_fd)
        finally:
            os.close(original_fd)
        table = {item['index']: item for item in source_boot._gpt(raw_fd)}
        partition_identities = {}
        for part in {root_part['index']: root_part, boot_part['index']: boot_part}.values():
            host.require(table.get(part['index']) == part, 'BPF retained partition geometry differs')
            part_path = root / ('partition-%d.raw' % part['index'])
            part_fd = source_boot._open_regular(part_path)
            try:
                part_before = source_boot._identity(os.fstat(part_fd))
                host.require(part_before[6] == part['size'], 'BPF retained partition size differs')
                offset = 0
                while offset < part['size']:
                    size = min(1024 * 1024, part['size'] - offset)
                    host.require(source_boot._pread(part_fd, offset, size) ==
                                 source_boot._pread(raw_fd, part['start'] + offset, size),
                                 'BPF retained partition bytes differ')
                    offset += size
                host.require(source_boot._identity(os.fstat(part_fd)) == part_before ==
                             source_boot._identity(os.stat(part_path, follow_symlinks=False)),
                             'BPF retained partition changed during inspection')
                partition_identities[part['index']] = part_before
            finally:
                os.close(part_fd)
        host.require(source_boot._identity(os.fstat(raw_fd)) == before ==
                     source_boot._identity(os.stat(raw_path, follow_symlinks=False)),
                     'BPF retained base image changed during inspection')
    finally:
        os.close(raw_fd)
    host.require(source_boot._identity(os.stat(root_image, follow_symlinks=False)) == partition_identities[root_part['index']],
                 'BPF verified package partition changed before inventory')
    inventories = source_boot._packages(root_image, env)
    host.require(source_boot._identity(os.stat(root_image, follow_symlinks=False)) == partition_identities[root_part['index']],
                 'BPF verified package partition changed during inventory')
    path = '/usr/lib/systemd/systemd'
    entry = source_boot._listing(root_image, ['/usr/lib/systemd'], env)['/usr/lib/systemd'].get('systemd')
    host.require(entry is not None and stat.S_ISREG(entry['mode']) and entry['mode'] & 0o111 and
                 entry['mode'] & 0o022 == 0 and 0 < entry['size'] <= source_boot.MAX_META,
                 'BPF original systemd executable identity invalid')
    executable = source_boot._cat(root_image, path, env)
    host.require(len(executable) == entry['size'] and len(executable) >= 20 and executable[:6] == b'\x7fELF\x02\x01' and
                 struct.unpack_from('<H', executable, 18)[0] == 62, 'BPF original systemd executable is not x86_64 ELF')
    status = source_boot._cat(root_image, '/var/lib/dpkg/status', env).decode('utf-8', 'strict')
    packages = []
    for paragraph in status.split('\n\n'):
        fields = {}
        for line in paragraph.splitlines():
            if not line.startswith((' ', '\t')) and ': ' in line:
                key, value = line.split(': ', 1)
                host.require(key not in fields, 'BPF package status field duplicated')
                fields[key] = value
        if fields.get('Package') == 'systemd':
            packages.append(fields)
    inventory = source_boot._cat(root_image, '/var/lib/dpkg/info/systemd.list', env).decode('utf-8', 'strict').splitlines()
    host.require(len(packages) == 1 and packages[0].get('Status') == 'install ok installed' and
                 packages[0].get('Architecture') == 'amd64' and type(packages[0].get('Version')) is str and
                 packages[0]['Version'] and len(inventory) == len(set(inventory)) and inventory.count(path) == 1,
                 'BPF original systemd package provenance invalid')
    expected_systemd = {'package': 'systemd', 'version': packages[0]['Version'], 'path': path,
                        'sha256': hashlib.sha256(executable).hexdigest()}
    host.require(type(meta['package_executables']) is dict and set(meta['package_executables']) == {'python', 'systemd'} and
                 meta['package_executables']['systemd'] == expected_systemd and
                 build['bpf_authority']['bootstrap_manifest']['package_executables']['systemd'] == expected_systemd,
                 'BPF original systemd package binding differs')
    entries = source_boot._walk_headers(root_image, inventories, env)
    headers = root / 'headers/usr/src'
    host.require(Path(meta['headers']) == headers, 'BPF selected header directory differs')
    digest = hashlib.sha256()
    for path, item in sorted(entries.items()):
        if stat.S_ISDIR(item['mode']):
            target = headers / path.removeprefix('/usr/src/')
            info = os.lstat(target)
            host.require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and
                         stat.S_IMODE(info.st_mode) == 0o700, 'BPF selected header directory changed')
            digest.update((path + '\0directory\0' + str(item['mode']) + '\n').encode())
    for path, item in sorted(entries.items()):
        if stat.S_ISREG(item['mode']):
            target = headers / path.removeprefix('/usr/src/')
            fd = source_boot._open_regular(target)
            try:
                before = os.fstat(fd)
                host.require(before.st_size == item['size'] and stat.S_IMODE(before.st_mode) == stat.S_IMODE(item['mode']) and
                             source_boot._identity(before) == source_boot._identity(os.stat(target, follow_symlinks=False)),
                             'BPF selected header file changed')
                sha = source_boot._hash_fd(fd)
            finally:
                os.close(fd)
            digest.update((path + '\0' + str(item['mode']) + '\0' + sha + '\n').encode())
    for path, item in sorted(entries.items()):
        if stat.S_ISLNK(item['mode']):
            target = headers / path.removeprefix('/usr/src/')
            host.require(stat.S_ISLNK(os.lstat(target).st_mode) and os.readlink(target) == item['target'],
                         'BPF selected header link changed')
            kind = 'dangling' if item.get('dangling') is True else 'link'
            digest.update((path + '\0' + kind + '\0' + item['target'] + '\n').encode())
    header_sha = digest.hexdigest()
    host.require(header_sha == meta['header_manifest_sha256'], 'BPF selected header inventory differs')
    kernel = source_bpf_build._input_bytes(root / 'boot/vmlinuz', meta['kernel']['sha256'], 1024 ** 3)
    config = source_bpf_build._input_bytes(root / 'boot/config', meta['config']['sha256'], 1024 * 1024)
    system_map = source_bpf_build._input_bytes(root / 'boot/System.map', meta['system_map']['sha256'], 32 * 1024 ** 2)
    symvers = source_bpf_build._input_bytes(headers / 'linux-headers-6.8.0-147-generic/Module.symvers',
                                            meta['module_symvers_sha256'], 32 * 1024 ** 2)
    host.require(kernel == source_bpf_build._input_bytes(attempt / 'boot/vmlinuz', build['boot_inputs']['kernel_sha256'], 1024 ** 3),
                 'BPF selected boot kernel differs from original extraction')
    directory = str(Path(meta['initrd_source']).parent)
    original_kernel = _original_package_kernel(host, boot_image, partition_identities[boot_part['index']],
                                               directory, inventories, kernel, meta['kernel']['sha256'])
    host.require(meta['kernel']['sha256'] == build['boot_inputs']['kernel_sha256'],
                 'BPF original package kernel digest differs from boot input')
    launched_kernel = source_vm._boot_paths(host, build)['kernel']
    host.require(len(original_kernel) == build['boot_inputs']['kernel_bytes'] and
                 original_kernel == source_bpf_build._input_bytes(launched_kernel, build['boot_inputs']['kernel_sha256'], 1024 ** 3),
                 'BPF actual direct-boot kernel differs from original package bytes')
    host.require(config == source_boot._cat(boot_image, directory.rstrip('/') + '/config-' + source_boot.RELEASE, env, maximum=1024 * 1024) and
                 system_map == source_boot._cat(boot_image, directory.rstrip('/') + '/System.map-' + source_boot.RELEASE, env),
                 'BPF original boot inputs differ from selected partition')
    binding = source_kernel_observer.binding_bytes(source_kernel_observer.observer_bindings(
        kernel, system_map, config, header_sha, symvers))
    expected = build['boot_inputs']['observer_bindings_sha256']
    host.require(hashlib.sha256(binding).hexdigest() == expected and
                 source_bpf_build._input_bytes(attempt / 'observer-bindings.json', expected, 16384) == binding,
                 'BPF observer binding differs from selected source inputs')
    selected = host.strict_json(binding)
    decoded = source_kernel_observer.decode_kernel(kernel)
    _independent_layout(host, decoded['btf'], selected['layout'])
    entries = [line.split() for line in system_map.decode('ascii', 'strict').splitlines()]
    offload = [row for row in entries if len(row) == 3 and row[2] == 'bpf_map_offload_ops']
    anchor = [row for row in entries if len(row) == 3 and row[2] == 'idr_alloc_cyclic']
    symbol = [row for row in selected['symbols'] if row['name'] == 'bpf_map_offload_ops']
    host.require(len(offload) == len(anchor) == len(symbol) == 1 and offload[0][1] == 'D' and
                 anchor[0][1] == 'T' and symbol[0]['type'] == 'D' and
                 symbol[0]['delta'] == int(offload[0][0], 16) - int(anchor[0][0], 16),
                 'BPF independent offload symbol differs')
    address = int(offload[0][0], 16)
    section = decoded['sections'].get('.rodata')
    host.require(section is not None and section['type'] == 1 and section['flags'] & 2 and address % 8 == 0 and
                 section['address'] <= address and address + 8 <= section['address'] + section['size'] and
                 section['offset'] + address - section['address'] + 8 <= len(decoded['elf']),
                 'BPF independent offload symbol is outside file-backed rodata')
    module = attempt / 'native/g2_bpf_guard.ko'
    raw = source_bpf_build._input_bytes(module, build['bpf_authority']['native_artifacts']['module_sha256'], 64 * 1024 ** 2)
    host.require(len(raw) >= 64 and raw[:7] == b'\x7fELF\x02\x01\x01' and
                 struct.unpack_from('<HHI', raw, 16) == (1, 62, 1), 'BPF selected module ELF invalid')
    offset = struct.unpack_from('<Q', raw, 40)[0]
    width, count, names_index = struct.unpack_from('<HHH', raw, 58)
    host.require(width == 64 and 0 < count <= 4096 and 0 < names_index < count and
                 offset <= len(raw) and count * width <= len(raw) - offset, 'BPF module section table invalid')
    sections = [struct.unpack_from('<IIQQQQIIQQ', raw, offset + index * width) for index in range(count)]
    names = sections[names_index]
    host.require(names[1] == 3 and names[4] <= len(raw) and names[5] <= len(raw) - names[4],
                 'BPF module section names invalid')
    strings = raw[names[4]:names[4] + names[5]]
    matches = []
    for section in sections:
        host.require(section[0] < len(strings), 'BPF module section name offset invalid')
        end = strings.find(b'\0', section[0])
        host.require(end >= 0, 'BPF module section name is unterminated')
        if strings[section[0]:end] == b'.g2_observer_binding':
            matches.append(section)
    host.require(len(matches) == 1, 'BPF module observer section is missing or duplicated')
    section = matches[0]
    host.require(section[1] == 1 and not section[2] & 1 and section[5] == len(binding) and
                 section[4] <= len(raw) and section[5] <= len(raw) - section[4] and
                 raw[section[4]:section[4] + section[5]] == binding, 'BPF module observer section differs')


def _kernel_btf(host, build):
    # The target is the selected direct-boot kernel, never the host kernel.
    import source_vm
    path = source_vm._boot_paths(host, build)['kernel']
    inputs = build['boot_inputs']
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        host.require(0 < before.st_size == inputs['kernel_bytes'] <= 1024 ** 3,
                     'BPF selected kernel size differs')
        data = bytearray()
        while len(data) < before.st_size:
            part = os.read(fd, min(1024 * 1024, before.st_size - len(data)))
            host.require(bool(part), 'BPF selected kernel read was short')
            data.extend(part)
        after = os.fstat(fd)
        named = os.stat(path, follow_symlinks=False)
        ident = lambda st: (st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
        host.require(ident(before) == ident(after) == ident(named) and
                     hashlib.sha256(data).hexdigest() == inputs['kernel_sha256'],
                     'BPF selected kernel changed or has wrong digest')
    finally:
        os.close(fd)
    import source_kernel_observer
    try:
        return source_kernel_observer.decode_kernel(bytes(data))['btf']
    except (ValueError, UnicodeError, struct.error) as exc:
        raise host.Failure('BPF selected kernel binding is invalid') from exc


def _device_program():
    # Independently encode the pinned runc clean-rules generator.
    rules = ((1, -1, -1, 1), (2, -1, -1, 1), (2, 1, 3, 7), (2, 1, 5, 7),
             (2, 1, 7, 7), (2, 1, 8, 7), (2, 1, 9, 7), (2, 5, 0, 7),
             (2, 5, 1, 7), (2, 5, 2, 7), (2, 10, 200, 7), (2, 136, -1, 7))
    def ins(op, dst=0, src=0, offset=0, immediate=0):
        return struct.pack('<BBhi', op, dst | src << 4, offset, immediate)
    result = [ins(0x61, 2, 1), ins(0x54, 2, immediate=0xffff), ins(0x61, 3, 1),
              ins(0x74, 3, immediate=16), ins(0x61, 4, 1, 4), ins(0x61, 5, 1, 8)]
    for typ, major, minor, access in rules:
        block = [(0x55, 2, 0, typ)]
        if access != 7:
            block.extend(((0xbc, 1, 3, 0), (0x54, 1, 0, access), (0x5d, 1, 3, 0)))
        if major >= 0:
            block.append((0x55, 4, 0, major))
        if minor >= 0:
            block.append((0x55, 5, 0, minor))
        block.extend(((0xb4, 0, 0, 1), (0x95, 0, 0, 0)))
        for index, (op, dst, src, imm) in enumerate(block):
            offset = len(block) - index - 1 if op in (0x55, 0x5d) else 0
            result.append(ins(op, dst, src, offset, imm))
    result.extend((ins(0xb4), ins(0x95)))
    return b''.join(result)


def _runtime_program(host, body):
    attr = bytes.fromhex(body['attributes']['attr_hex'])
    host.require(24 <= len(attr) <= 4096 and body['input_objects'] == [], 'BPF runtime program inputs invalid')
    full = attr.ljust(4096, b'\0')
    typ, count = struct.unpack_from('<II', full)
    log_level, log_size = struct.unpack_from('<II', full, 24)
    host.require(0 < count <= 4096 and ((log_level == log_size == 0) or
                 (log_level == 1 and log_size == 131072)), 'BPF runtime program log or count invalid')
    program = bytes.fromhex(body['program_bytes']) if body['program_bytes'] is not None else b''
    probe = struct.pack('<BBhi', 0xb7, 0, 0, 0) + struct.pack('<BBhi', 0x95, 0, 0, 0)
    host.require(len(program) == count * 8 and
                 ((typ == 15 and program == _device_program() and body['license'] == 'Apache') or
                  (typ in (1, 15) and program == probe and body['license'] == 'MIT')),
                 'BPF runtime program is not the fixed device policy or allowed probe')
    expected = bytearray(4096)
    struct.pack_into('<II', expected, 0, typ, count)
    struct.pack_into('<II', expected, 24, log_level, log_size)
    if typ == 15:
        func_size, line_size = struct.unpack_from('<I', full, 76)[0], struct.unpack_from('<I', full, 92)[0]
        host.require((func_size, line_size) == (0, 0) or
                     ((func_size, line_size) == (8, 16) and len(attr) >= 108),
                     'BPF runtime program attributes or normalized pointers differ')
        if (func_size, line_size) == (8, 16):
            struct.pack_into('<I', expected, 76, 8)
            struct.pack_into('<I', expected, 92, 16)
    if typ == 1:
        struct.pack_into('<I', expected, 72, (1 << 32) - 1)
        struct.pack_into('<I', expected, 88, 1)
    host.require(full == bytes(expected), 'BPF runtime program attributes or normalized pointers differ')


def _runtime_command(host, body):
    command = body['command']
    attr = bytes.fromhex(body['attributes']['attr_hex'])
    full = attr.ljust(4096, b'\0')
    inputs = body['input_objects']
    output = body['output']
    result = body['result']
    allowed_outputs = {'MAP_CREATE': ('object_cookie',), 'PROG_LOAD': ('object_cookie', 'log_true_size'),
                       'PROG_QUERY': ('attach_flags', 'program_ids', 'program_attach_flags'),
                       'PROG_GET_FD_BY_ID': ('object_cookie',), 'OBJ_GET_INFO_BY_FD': ('info_len', 'object_cookie', 'type', 'kernel_id', 'attributes'),
                       'PROG_ATTACH': ('attachment_id',), 'PROG_DETACH': ('attachment_id',)}
    host.require(command in allowed_outputs and (output is None or _shape(output, allowed_outputs[command])) and
                 (result is None or result < 0 or output is not None), 'BPF runtime output schema or result invalid')
    if output is not None:
        for field in ('object_cookie', 'attachment_id'):
            if field in output:
                host.require(output[field] is None or _integer(output[field], 1), 'BPF output object identity invalid')
        if 'log_true_size' in output:
            host.require(_integer(output['log_true_size'], high=(1 << 32) - 1), 'BPF verifier log size invalid')
    if command == 'PROG_LOAD':
        _runtime_program(host, body)
        return
    if command == 'MAP_CREATE':
        expected = bytearray(4096)
        struct.pack_into('<IIII', expected, 0, 2, 4, 4, 1)
        expected[28:40] = b'feature_test'
        host.require(len(attr) >= 44 and full == expected and inputs == [], 'BPF feature map attributes differ')
    elif command == 'PROG_GET_FD_BY_ID':
        host.require(len(attr) >= 12 and _integer(struct.unpack_from('<I', full)[0], 1) and
                     not any(full[4:]) and inputs == [], 'BPF program ID request differs')
    elif command == 'OBJ_GET_INFO_BY_FD':
        size = struct.unpack_from('<I', full, 4)[0]
        host.require(len(attr) >= 16 and not any(full[:4]) and not any(full[8:]) and len(inputs) == 1 and
                     inputs[0]['field'] == 'info.bpf_fd' and inputs[0]['kind'] in ('program', 'map') and
                     (232 if inputs[0]['kind'] == 'program' else 88) <= size <= 65536,
                     'BPF scalar object-info request differs')
        if output is not None:
            host.require(_integer(output['info_len'], 1, size) and output['object_cookie'] == inputs[0]['cookie'] and
                         _integer(output['type'], 1, (1 << 32) - 1) and _integer(output['kernel_id'], 1, (1 << 32) - 1) and
                         type(output['attributes']) is dict, 'BPF scalar object-info output differs')
    elif command == 'PROG_QUERY':
        capacity = struct.unpack_from('<I', full, 24)[0]
        expected = bytearray(4096)
        struct.pack_into('<I', expected, 4, 6)
        struct.pack_into('<I', expected, 24, capacity)
        host.require(len(attr) >= 28 and capacity <= 64 and full == expected and len(inputs) == 1 and
                     inputs[0]['field'] == 'query.target_fd' and inputs[0]['kind'] == 'cgroup',
                     'BPF program query request differs')
        if output is not None:
            host.require(_integer(output['attach_flags'], high=(1 << 32) - 1) and type(output['program_ids']) is list and
                         type(output['program_attach_flags']) is list and len(output['program_ids']) <= capacity and
                         len(output['program_attach_flags']) in (0, len(output['program_ids'])) and
                         all(_integer(value, 1, (1 << 32) - 1) for value in output['program_ids']) and
                         all(_integer(value, high=(1 << 32) - 1) for value in output['program_attach_flags']),
                         'BPF program query output invalid')
    else:
        flags = struct.unpack_from('<I', full, 12)[0]
        expected = bytearray(4096)
        struct.pack_into('<II', expected, 8, 6, flags)
        host.require(len(attr) >= 16 and full == expected and
                     (flags in (2, 6) if command == 'PROG_ATTACH' else flags == 0),
                     'BPF attach request attributes differ')
        fields = [('target_fd', ('cgroup', 'dev_null')), ('attach_bpf_fd', ('program',))]
        if command == 'PROG_ATTACH' and flags == 6:
            fields.append(('replace_bpf_fd', ('program',)))
        host.require(len(inputs) == len(fields) and all(item['field'] == field and item['kind'] in kinds
                     for item, (field, kinds) in zip(inputs, fields)), 'BPF attach input descriptors differ')


def _replay_programs(host, records):
    device_by_invocation = {}
    for event in records:
        if event['kind'] == 'transaction':
            body = event['body']
            if body['grant_id'] is not None or body['command'] == 'PROG_QUERY':
                _runtime_command(host, body)
            if (body['grant_id'] is not None and body['command'] == 'PROG_LOAD' and
                    body['state'] == 'retired' and body['result'] is not None and
                    body['program_bytes'] == _device_program().hex()):
                full = bytes.fromhex(body['attributes']['attr_hex']).ljust(4096, b'\0')
                log_level, log_size = struct.unpack_from('<II', full, 24)
                log_true_size = body['output']['log_true_size'] if type(body['output']) is dict else None
                inv_id = body['invocation_id']
                prior = device_by_invocation.setdefault(inv_id, [])
                host.require(len(prior) < 2, 'BPF runtime device program load exceeded retry bound')
                if not prior:
                    host.require(log_level == 0 and log_size == 0, 'BPF initial device load used non-zero verifier log')
                else:
                    prev_res, prev_log_size, prev_true_size = prior[0]
                    host.require(prev_res < 0 and prev_log_size == 0 and prev_true_size == 0 and
                                 log_level == 1 and log_size == 131072,
                                 'BPF second device load did not follow a failed level-0 load with log_true_size=0')
                prior.append((body['result'], log_size, log_true_size))

def _daemon_transition(host, previous, current):
    helper = hashlib.sha256(b'\0'.join(arg.encode('ascii') for arg in _DAEMON_HELPER_ARGV) + b'\0').hexdigest()
    executing = hashlib.sha256(b'\0'.join(arg.encode('ascii') for arg in _DOCKERD_ARGV) + b'\0').hexdigest()
    if previous is None:
        host.require(current['state'] == 'stopped' and current['argv_sha256'] == helper,
                     'BPF daemon was not registered with its fixed stopped helper')
        return
    transitions = {'stopped': ('stopped', 'executing', 'exited'),
                   'executing': ('executing', 'exited'), 'exited': ('exited',)}
    expected = previous['argv_sha256'] if current['state'] == 'exited' else (
        helper if current['state'] == 'stopped' else executing)
    host.require(all(previous[field] == current[field] for field in previous
                     if field not in ('state', 'argv_sha256')) and
                 current['state'] in transitions[previous['state']] and current['argv_sha256'] == expected,
                 'BPF daemon identity, fixed arguments or state changed')


def _runtime_exit_success(host, status):
    host.require(type(status) is int and 0 <= status <= 0xffff and
                 ((status & 0xff) == 0 or
                  (status <= 0xff and 1 <= (status & 0x7f) <= 64)),
                 'BPF runtime task exit has an invalid Linux wait status')
    return status == 0


def _replay_runtime_root(host, records, prefix, build):
    systemd = build['bpf_authority']['bootstrap_manifest']['package_executables']['systemd']['sha256']
    broker, root, pid1, pair = None, None, None, None
    for event in records:
        kind, body = event['kind'], event['body']
        if kind == 'principal' and body['role'] == 'BOOTSTRAP_BROKER':
            host.require(body['id'] == prefix['broker_principal'] and body['registration'] == 'bootstrap' and
                         (broker is None or all(body[key] == broker[key] for key in
                          ('id', 'task_cookie', 'parent_cookie', 'tgid', 'tid', 'start_ns'))),
                         'BPF original bootstrap broker identity changed')
            broker = body
        elif kind == 'audit_control' and body['phase'] in ('early_setup', 'final_rules', 'final_lock'):
            actor = body['actor']
            identity = tuple(actor[key] for key in ('task_cookie', 'tgid', 'tid', 'start_ns'))
            host.require(broker is not None and actor['task_cookie'] == broker['parent_cookie'] and
                         actor['tgid'] == actor['tid'] == 1 and (pid1 is None or identity == pid1),
                         'BPF bootstrap PID1 control identity differs')
            pid1 = identity
        elif kind == 'runtime_root':
            _record_body(host, kind, body)
            host.require(root is None and broker is not None and pid1 is not None and broker['terminal'] is None and
                         body['broker_cookie'] == broker['task_cookie'] and body['pid1_cookie'] == broker['parent_cookie'] and
                         (body['pid1_cookie'], 1, 1, body['pid1_start_ns']) == pid1 and
                         body['pid1_exe_sha256'] == systemd and broker['tgid'] in body['thread_ids'],
                         'BPF runtime root broker, parent, digest or handoff differs')
            root = body
        elif kind == 'containerd_launcher':
            host.require(root is not None and pair is None and body['exe_sha256'] == prefix['broker_sha256'] and
                         body['target_sha256'] == build['bpf_authority']['bootstrap_manifest']['package_executables']['containerd']['sha256'],
                         'BPF containerd launcher binding differs')
            pair = body
        elif kind == 'guard' and body['record']['kind'] == 12:
            host.require(root is not None and pair is not None, 'BPF containerd launcher binding precedes watch')
        elif (kind == 'principal' and not (body['registration'] == 'bootstrap' and body['role'] in
              ('BOOTSTRAP_BROKER', 'BOOTSTRAP_AUDIT_READER', 'BOOTSTRAP_TERMINAL_OBSERVER')) or
              kind in ('grant', 'invocation', 'daemon', 'containerd_launcher', 'attachment', 'runtime_exec', 'cgroup_enrollment',
                       'notification_denial', 'notification_cancel', 'notification_response', 'grant_object_cleanup',
                       'exec_admission', 'endpoint_transition', 'selftest_fd_stage', 'selftest_runtime_stage') or
              kind == 'held_file' and body['source'] == 'runtime_import' or
              kind == 'registry' and body['grant_id'] is not None or
              kind == 'transaction' and (body['grant_id'] is not None or body['invocation_id'] is not None)):
            host.require(root is not None, 'BPF runtime admission or effect precedes root handoff')


def _replay_ancestry(host, records, prefix, build):
    packages = build['bpf_authority']['bootstrap_manifest']['package_executables']
    pins = {name: packages[name]['sha256'] for name in ('dockerd', 'containerd', 'shim', 'runc')}
    pins['wrapper'] = prefix['broker_sha256']
    host.require(all(_hex(value) for value in pins.values()) and len(set(pins.values())) == len(pins),
                 'BPF pinned executable identities are ambiguous')
    by_digest = {digest: role for role, digest in pins.items()}
    guards, tasks, daemons, identities, principals, invocations = {}, {}, {}, {}, {}, {}
    listeners = {}
    runtime_execs = {}
    observed_retirement_actors, delete_permits = set(), {}
    pair, watch_daemons = None, {}
    for event in records:
        kind, body = event['kind'], event['body']
        if kind == 'containerd_launcher':
            host.require(pair is None and body['exe_sha256'] == pins['wrapper'] and
                         body['target_sha256'] == pins['containerd'], 'BPF containerd launcher binding differs')
            pair = body
            launcher_key = (body['exe_dev'], body['exe_ino'])
            target_key = (body['target_dev'], body['target_ino'])
            host.require(identities.setdefault(launcher_key, body['exe_sha256']) == body['exe_sha256'] and
                         identities.setdefault(target_key, body['target_sha256']) == body['target_sha256'],
                         'BPF containerd launcher binding identity differs')
            continue
        if kind == 'guard':
            guard = body['record']
            if guard['kind'] == 5:
                for invocation_id, permit in delete_permits.items():
                    invocation = invocations[invocation_id]
                    if (guard['task_cookie'] == invocation['task_cookie'] and
                            guard['root_cookie'] == permit['key']['root_kn_id'] and
                            guard['cgroup_id'] == permit['key']['kn_id']):
                        task = tasks.get(invocation['task_cookie'])
                        host.require(task is not None and guard['task_start_ns'] == task['start'] and
                                     task['key'] == (permit['value']['exe_dev'], permit['value']['exe_ino']) and
                                     task['exec'] > invocation['ancestry'][-1]['exec_sequence'],
                                     'BPF delete offline callback differs from the authorized runc incarnation')
            if guard['kind'] not in (9, 10, 11, 12):
                continue
            guards[guard['sequence']] = guard
            cookie, detail = guard['task_cookie'], guard['detail']
            key = (detail['exe_dev'], detail['exe_ino'])
            host.require(cookie > 0 and guard['task_start_ns'] > 0 and detail['tgid'] > 0 and
                         detail['tid'] > 0 and key[1] > 0 and detail['exec_generation'] > 0,
                         'BPF ancestry task identity invalid')
            if guard['kind'] == 12:
                host.require(pair is not None and cookie not in tasks and detail['exec_generation'] == 1 and
                             key != launcher_key, 'BPF daemon watch lacks unique launcher binding')
                tasks[cookie] = {'parent': guard['parent_cookie'], 'start': guard['task_start_ns'],
                                 'tgid': detail['tgid'], 'tid': detail['tid'], 'generation': 1,
                                 'key': key, 'fork': guard['sequence'], 'exec': guard['sequence'], 'exited': False,
                                 'root': cookie, 'managed': False, 'managed_versions': {1: False},
                                 'versions': {1: (key, guard['sequence'])}}
            elif guard['kind'] == 9:
                parent = tasks.get(guard['parent_cookie'])
                host.require(cookie not in tasks and parent is not None and not parent['exited'] and
                             detail['exec_generation'] == parent['generation'] and key == parent['key'],
                             'BPF fork does not inherit its observed parent')
                tasks[cookie] = {'parent': guard['parent_cookie'], 'start': guard['task_start_ns'],
                                 'tgid': detail['tgid'], 'tid': detail['tid'], 'generation': parent['generation'],
                                 'key': key, 'fork': guard['sequence'], 'exec': parent['exec'], 'exited': False,
                                 'root': parent['root'], 'managed': parent['managed'],
                                 'managed_versions': {parent['generation']: parent['managed']},
                                 'versions': {parent['generation']: (key, parent['exec'])}}
            else:
                task = tasks.get(cookie)
                host.require(task is not None and not task['exited'] and task['parent'] == guard['parent_cookie'] and
                             task['start'] == guard['task_start_ns'] and task['tgid'] == detail['tgid'] and
                             task['tid'] == detail['tid'], 'BPF task incarnation changed')
                if guard['kind'] == 10:
                    host.require(detail['exec_generation'] == task['generation'] + 1, 'BPF task exec generation differs')
                    root = tasks[task['root']]
                    daemon = watch_daemons.get(task['root'])
                    if cookie == task['root'] and task['generation'] == 1:
                        host.require(daemon is not None and key != launcher_key and key != target_key,
                                     'BPF daemon dockerd transition lacks launcher binding')
                        host.require(identities.setdefault(key, pins['dockerd']) == pins['dockerd'],
                                     'BPF daemon dockerd identity differs')
                        root['dockerd_key'] = key
                    if key == launcher_key:
                        parent = tasks.get(task['parent'])
                        host.require(daemon is not None and cookie != task['root'] and task['tgid'] == task['tid'] and
                                     task['tgid'] != root['tgid'] and task['key'] == root.get('dockerd_key') and
                                     task['exec'] <= task['fork'] and parent is not None and not parent['exited'] and
                                     parent['tgid'] == root['tgid'] and parent['key'] == root.get('dockerd_key'),
                                     'BPF containerd launcher transition differs')
                    if key == target_key:
                        host.require((task['key'] == launcher_key and task['tgid'] == task['tid'] and
                                      task['exec'] > task['fork']) or
                                     (task['key'] == target_key and task['managed']),
                                     'BPF containerd launcher target transition differs')
                        task['managed'] = True
                    for invocation in invocations.values():
                        if invocation['task_cookie'] == cookie:
                            host.require(detail['exec_generation'] == invocation['ancestry'][-1]['exec_generation'] + 1,
                                         'BPF runtime executed an unapproved later image')
                    task.update(generation=detail['exec_generation'], key=key, exec=guard['sequence'])
                    task['versions'][detail['exec_generation']] = (key, guard['sequence'])
                    task['managed_versions'][detail['exec_generation']] = task['managed']
                else:
                    host.require(detail['exec_generation'] == task['generation'] and key == task['key'],
                                 'BPF task exit identity differs')
                    task['exited'] = True
                    task['exit_sequence'] = guard['sequence']
                    task['exit_code'] = detail['exit_code']
        elif kind == 'principal':
            principals[body['id']] = body
        elif kind == 'daemon':
            watch = guards.get(body['watch_sequence'])
            host.require(watch is not None and watch['kind'] == 12 and watch['task_cookie'] == body['task_cookie'] and
                         watch['task_start_ns'] == body['start_ns'] and body['helper_sha256'] == prefix['broker_sha256'] and
                         body['dockerd_sha256'] == pins['dockerd'], 'BPF daemon lacks its pinned task watch')
            watch_key = (watch['detail']['exe_dev'], watch['detail']['exe_ino'])
            host.require(pair is not None and watch_key != launcher_key and
                         (body['task_cookie'] not in watch_daemons or
                          watch_daemons[body['task_cookie']]['cookie'] == body['cookie']),
                         'BPF daemon launcher binding differs')
            watch_daemons[body['task_cookie']] = body
            host.require(identities.setdefault(watch_key, body['helper_sha256']) == body['helper_sha256'],
                         'BPF daemon helper executable changed')
            daemons[body['cookie']] = body
        elif kind == 'invocation':
            if body['listener_id'] is not None:
                host.require(listeners.setdefault(body['listener_id'], body['id']) == body['id'],
                             'BPF listener identity was reused by another invocation')
            daemon, nodes = daemons.get(body['daemon_cookie']), body['ancestry']
            principal = principals.get(body['principal_id'])
            leader = tasks.get(body['task_cookie'])
            host.require(principal is not None and leader is not None and
                         principal['task_cookie'] == body['task_cookie'] and principal['start_ns'] == leader['start'] and
                         principal['tgid'] == principal['tid'] == leader['tgid'] == leader['tid'],
                         'BPF runtime principal differs from observed task incarnation')
            host.require(daemon is not None and nodes and nodes[0]['task_cookie'] == daemon['task_cookie'] and
                         nodes[-1]['task_cookie'] == body['task_cookie'], 'BPF ancestry endpoints differ')
            roles = []
            for index, node in enumerate(nodes):
                task = tasks.get(node['task_cookie'])
                host.require(task is not None and
                             (index + 1 < len(nodes) or body['state'] == 'running' or
                              task['exited'] == (body['state'] in ('settled', 'failed'))) and
                             node['parent_cookie'] == task['parent'] and node['exec_generation'] in task['versions'] and
                             node['fork_sequence'] == task['fork'] and
                             node['exec_sequence'] == task['versions'][node['exec_generation']][1] and
                             (index == 0 or node['parent_cookie'] == nodes[index - 1]['task_cookie']) and
                             node['exe_sha256'] in by_digest, 'BPF ancestry differs from observed task lineage')
                version_key = task['versions'][node['exec_generation']][0]
                host.require(version_key != launcher_key and
                             (node['exe_sha256'] != pins['containerd'] or
                              version_key == target_key and task['managed_versions'][node['exec_generation']]),
                             'BPF containerd launcher ancestry differs')
                prior = identities.setdefault(task['versions'][node['exec_generation']][0], node['exe_sha256'])
                host.require(prior == node['exe_sha256'], 'BPF executable inode acquired a different digest')
                roles.append(by_digest[node['exe_sha256']])
            collapsed = [role for index, role in enumerate(roles) if index == 0 or role != roles[index - 1]]
            if body['state'] in ('settled', 'failed'):
                success = _runtime_exit_success(host, tasks[body['task_cookie']]['exit_code'])
                host.require(body['state'] != 'settled' or success,
                             'BPF runtime success contradicts the actual exit status')
            endpoint = tasks[body['task_cookie']]
            host.require(roles[-1] == 'wrapper' and
                         (body['state'] not in ('running', 'settled') or
                          body['id'] in runtime_execs and endpoint['generation'] == nodes[-1]['exec_generation'] + 1 and
                          endpoint['exec'] == runtime_execs[body['id']]['guard_sequence']),
                         'BPF runtime lacks the observed post-bind executable transition')
            expected = ['dockerd', 'wrapper'] if body['command'] == 'features' else ['dockerd', 'containerd', 'shim', 'wrapper']
            host.require(collapsed == expected and nodes[0]['fork_sequence'] == daemon['watch_sequence'] and
                         daemon['helper_sha256'] == prefix['broker_sha256'] and daemon['dockerd_sha256'] == pins['dockerd'],
                         'BPF ancestry does not follow the fixed daemon chain')
            invocations[body['id']] = body
        elif kind == 'runtime_exec':
            invocation = invocations.get(body['invocation_id'])
            guard = guards.get(body['guard_sequence'])
            task = tasks.get(body['task_cookie'])
            host.require(invocation is not None and invocation['state'] in ('bound', 'running') and
                         body['invocation_id'] not in runtime_execs and body['task_cookie'] == invocation['task_cookie'] and
                         guard is not None and guard['kind'] == 10 and guard['task_cookie'] == body['task_cookie'] and
                         task is not None and guard['task_start_ns'] == task['start'] and
                         guard['detail']['exec_generation'] == body['exec_generation'] ==
                         invocation['ancestry'][-1]['exec_generation'] + 1 == task['generation'] and
                         task['exec'] == body['guard_sequence'] and body['exe_sha256'] == pins['runc'],
                         'BPF runtime executable attestation differs from native exec and pinned image')
            host.require(identities.setdefault(task['key'], body['exe_sha256']) == body['exe_sha256'],
                         'BPF runtime executable inode acquired a different digest')
            permit = delete_permits.get(body['invocation_id'])
            if permit is not None:
                host.require(task['key'] == (permit['value']['exe_dev'], permit['value']['exe_ino']),
                             'BPF delete exec differs from its preauthorized runc identity')
            runtime_execs[body['invocation_id']] = body
        elif kind == 'registry' and body['map'] == 'retire_tasks' and body['action'] == 'insert':
            value = body['value']
            actor_id = value['invocation_id']
            executable = (value['exe_dev'], value['exe_ino'])
            if body['parent_transaction_id'] is not None and actor_id:
                invocation = invocations.get(actor_id)
                execution = runtime_execs.get(actor_id)
                exec_guard = None if execution is None else guards.get(execution['guard_sequence'])
                host.require(invocation is not None and invocation['grant_id'] == body['grant_id'] and
                             exec_guard is not None and executable ==
                             (exec_guard['detail']['exe_dev'], exec_guard['detail']['exe_ino']) and
                             identities.get(executable) == pins['runc'],
                             'BPF prior retirement actor lacks its pinned native runc execution')
                observed_retirement_actors.add((body['grant_id'], value['enrollment'], executable))
            elif body['parent_transaction_id'] is None and actor_id:
                invocation = invocations.get(actor_id)
                principal = None if invocation is None else principals.get(invocation['principal_id'])
                task = None if invocation is None else tasks.get(invocation['task_cookie'])
                host.require(invocation is not None and invocation['command'] == 'delete' and
                             invocation['state'] == 'bound' and invocation['grant_id'] == body['grant_id'] and
                             principal is not None and task is not None and not task['exited'] and
                             body['key']['tgid'] == principal['tgid'] == task['tgid'] == task['tid'] and
                             body['key']['leader_start_ns'] == principal['start_ns'] == task['start'] and
                             task['exec'] == invocation['ancestry'][-1]['exec_sequence'] and
                             (body['grant_id'], value['enrollment'], executable) in observed_retirement_actors and
                             identities.get(executable) == pins['runc'] and actor_id not in delete_permits,
                             'BPF delete permit lacks its bound wrapper and prior pinned runc identity')
                delete_permits[actor_id] = body
        elif kind == 'notification_denial':
            invocation = invocations.get(body['invocation_id'])
            requester = tasks.get(body['requester_cookie'])
            host.require(invocation is not None and invocation['listener_id'] is not None and
                         requester is not None and requester['fork'] <= body['guard_before'] and
                         (not requester['exited'] or requester['exit_sequence'] > body['guard_before']) and
                         body['guard_after'] >= body['guard_before'],
                         'BPF denial lacks its actual live notifying task and listener')
            cookie = body['requester_cookie']
            child = None
            while cookie != invocation['task_cookie']:
                node = tasks.get(cookie)
                host.require(node is not None and node['parent'] in tasks,
                             'BPF denial requester is outside the listener lineage')
                child, cookie = node, node['parent']
            if child is not None:
                execution = runtime_execs.get(invocation['id'])
                host.require(execution is not None and child['fork'] > execution['guard_sequence'],
                             'BPF denial descendant did not inherit the bound runtime listener')
        elif (kind in ('cgroup_enrollment', 'notification_cancel') or
              kind == 'held_file' and body['source'] == 'runtime_import' or
              kind == 'transaction' and body['state'] == 'prepared' and body['invocation_id'] is not None):
            invocation = invocations.get(body['invocation_id'])
            requester = tasks.get(body['requester_cookie'])
            leader = None if invocation is None else tasks.get(invocation['task_cookie'])
            principal = None if invocation is None else principals.get(invocation['principal_id'])
            host.require(invocation is not None and invocation['state'] in ('bound', 'running') and
                         requester is not None and leader is not None and principal is not None and
                         (kind == 'notification_cancel' or not requester['exited'] and not leader['exited']) and
                         requester['tgid'] == leader['tgid'] == principal['tgid'] and
                         requester['key'] == leader['key'] and invocation['id'] in runtime_execs and
                         leader['generation'] == invocation['ancestry'][-1]['exec_generation'] + 1 and
                         leader['exec'] == runtime_execs[invocation['id']]['guard_sequence'],
                         'BPF notifying task is not a live member of the bound runtime incarnation')


def _postgres_account(host, build):
    import source_boot

    path = host.PG_TAR
    fd = source_boot._open_regular(path)
    try:
        before = source_boot._identity(os.fstat(fd))
        host.require(source_boot._hash_fd(fd) == build['postgres_archive']['sha256'],
                     'BPF PostgreSQL image archive differs')
        with os.fdopen(os.dup(fd), 'rb') as stream, tarfile.open(fileobj=stream, mode='r:') as archive:
            manifest = host.strict_json(host._tar_member(archive, 'manifest.json', 1024 * 1024))
            host.require(type(manifest) is list and len(manifest) == 1 and type(manifest[0]) is dict,
                         'BPF PostgreSQL image manifest invalid')
            config_name, layers = manifest[0].get('Config'), manifest[0].get('Layers')
            config = host._tar_member(archive, config_name, 8 * 1024 * 1024)
            host.require(hashlib.sha256(config).hexdigest() == host.PG_ID.removeprefix('sha256:') and
                         type(layers) is list and layers and len(layers) == len(set(layers)) and
                         len(layers) == build['postgres_archive']['layers'], 'BPF PostgreSQL image configuration differs')
            files = {'etc/passwd': None, 'etc/group': None}
            for layer in layers:
                host.require(type(layer) is str and layer and not layer.startswith('/') and
                             '..' not in Path(layer).parts, 'BPF image layer path invalid')
                member = archive.getmember(layer)
                host.require(member.isfile(), 'BPF image layer is not a regular file')
                source = archive.extractfile(member)
                host.require(source is not None, 'BPF image layer is unreadable')
                updates, removed = {}, set()
                with source, tarfile.open(fileobj=source, mode='r:*') as inner:
                    for item in inner:
                        name = item.name
                        while name.startswith('./'):
                            name = name[2:]
                        host.require(not name.startswith('/') and '..' not in name.split('/'),
                                     'BPF image layer contains an unsafe path')
                        name = name.rstrip('/')
                        if name in ('.wh.etc', 'etc/.wh..wh..opq'):
                            removed.update(files)
                        elif name in ('etc/.wh.passwd', 'etc/.wh.group'):
                            removed.add('etc/' + name.rsplit('.wh.', 1)[1])
                        elif name == 'etc':
                            host.require(item.isdir(), 'BPF image account directory is not a directory')
                        elif name in files:
                            host.require(name not in updates and item.isfile() and 0 < item.size <= 1024 * 1024,
                                         'BPF image account file is invalid')
                            contents = inner.extractfile(item)
                            host.require(contents is not None, 'BPF image account file is unreadable')
                            with contents:
                                raw = contents.read(item.size + 1)
                            host.require(len(raw) == item.size, 'BPF image account file is truncated')
                            updates[name] = raw
                for name in removed:
                    files[name] = None
                files.update(updates)
        host.require(source_boot._identity(os.fstat(fd)) == before == source_boot._identity(os.stat(path, follow_symlinks=False)),
                     'BPF PostgreSQL image archive changed during inspection')
    finally:
        os.close(fd)
    host.require(all(type(value) is bytes for value in files.values()), 'BPF PostgreSQL image account is absent')
    passwd = [line.split(':') for line in files['etc/passwd'].decode('utf-8', 'strict').splitlines()]
    groups = [line.split(':') for line in files['etc/group'].decode('utf-8', 'strict').splitlines()]
    users = [row for row in passwd if row and row[0] == 'postgres']
    host.require(len(users) == 1 and len(users[0]) == 7 and users[0][2].isdecimal() and users[0][3].isdecimal(),
                 'BPF PostgreSQL image user is invalid')
    uid, gid = int(users[0][2]), int(users[0][3])
    host.require(0 < uid < (1 << 31) and 0 < gid < (1 << 31) and uid != 10002 and gid != 10002 and
                 any(len(row) == 4 and row[2].isdecimal() and int(row[2]) == gid for row in groups),
                 'BPF PostgreSQL image account identity is invalid')
    return uid, gid


def _bootstrap_template(purpose):
    image = 'sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610'
    no_log = {'Type': 'none', 'Config': {}}
    if purpose in ('postgres-uid', 'postgres-gid'):
        return {'Image': image, 'Entrypoint': ['/usr/bin/id'],
                'Cmd': ['-u' if purpose == 'postgres-uid' else '-g', 'postgres'],
                'AttachStdout': True, 'AttachStderr': True, 'Tty': False,
                'HostConfig': {'NetworkMode': 'none', 'ReadonlyRootfs': True, 'Privileged': False,
                               'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'],
                               'RestartPolicy': {'Name': 'no'}, 'LogConfig': no_log,
                               'Mounts': [{'Type': 'bind', 'Source': '/run/g2/postgres-owner-probe',
                                           'Target': '/var/lib/postgresql/data', 'ReadOnly': True}]}}
    if purpose == 'dns-preflight':
        return {'Image': image, 'Entrypoint': ['/opt/g2/dns-probe'], 'Cmd': [], 'User': '10002:10002',
                'Env': ['PATH=/usr/bin:/bin', 'LANG=C'], 'StopSignal': 'SIGTERM',
                'HostConfig': {'NetworkMode': 'g2-dns-preflight', 'Dns': ['127.0.0.1'],
                               'ReadonlyRootfs': True, 'Privileged': False, 'CapDrop': ['ALL'],
                               'SecurityOpt': ['no-new-privileges'], 'PidsLimit': 32,
                               'RestartPolicy': {'Name': 'no'}, 'LogConfig': no_log,
                               'Mounts': [{'Type': 'bind', 'Source': '/opt/g2/dns-probe',
                                           'Target': '/opt/g2/dns-probe', 'ReadOnly': True},
                                          {'Type': 'bind', 'Source': '/run/g2/dns-preflight',
                                           'Target': '/var/lib/postgresql/data', 'ReadOnly': False}]},
                'NetworkingConfig': {'EndpointsConfig': {'g2-dns-preflight':
                                    {'IPAMConfig': {'IPv4Address': '172.31.123.2'}}}}}
    return None


def _workload_template(purpose, uid, gid, browser_image):
    no_log = {'Type': 'none', 'Config': {}}
    if purpose == 'container-bpf-selftest':
        return {'Image': 'sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610',
                'Entrypoint': ['/opt/g2/g2-container-bpf-probe'],
                'Cmd': ['selftest', 'container-raw-denial'], 'User': '10002:10002',
                'HostConfig': {'NetworkMode': 'none', 'ReadonlyRootfs': True, 'Privileged': False,
                               'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'], 'PidsLimit': 32,
                               'RestartPolicy': {'Name': 'no'}, 'LogConfig': no_log,
                               'Mounts': [
                                   {'Type': 'bind', 'Source': '/opt/g2/g2-container-bpf-probe',
                                    'Target': '/opt/g2/g2-container-bpf-probe', 'ReadOnly': True},
                                   {'Type': 'bind', 'Source': '/run/g2/container-bpf.fifo',
                                    'Target': '/run/g2/container-bpf.fifo', 'ReadOnly': True}]}}
    if purpose == 'browser-negative':
        return {'Image': browser_image, 'Entrypoint': ['/opt/g2/browser/chrome-headless-shell'],
                'Cmd': ['--headless', '--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage',
                        '--disable-background-networking', '--no-first-run', '--no-default-browser-check',
                        '--disable-breakpad', '--disable-crash-reporter', '--user-data-dir=/tmp/g2-browser-profile',
                        '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=9222', 'about:blank'],
                'User': '10002:10002', 'Env': ['LD_LIBRARY_PATH=/opt/g2/browser:/opt/g2/runtime/lib',
                'HOME=/tmp', 'LANG=C', 'LC_ALL=C', 'PATH=/nonexistent'], 'WorkingDir': '/tmp',
                'HostConfig': {'NetworkMode': 'none', 'ReadonlyRootfs': True, 'Privileged': False,
                               'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'], 'PidsLimit': 128,
                               'Memory': 805306368, 'NanoCpus': 1000000000, 'RestartPolicy': {'Name': 'no'},
                               'LogConfig': no_log, 'Tmpfs': {'/tmp': 'rw,nosuid,nodev,noexec,size=134217728,mode=1777'}}}
    init = purpose.startswith('postgres-init-')
    if init:
        name = purpose.removeprefix('postgres-init-')
    elif purpose.startswith('postgres-live-'):
        name = purpose.removeprefix('postgres-live-')
    elif purpose.startswith('recovery-'):
        name = purpose.removeprefix('recovery-')
    else:
        return None
    hosts = {'app': '172.30.80.2', 'w1': '172.30.82.2', 'w2': '172.30.82.3', 'control': '172.30.82.4'}
    if name not in hosts:
        return None
    mounts = [{'Type': 'bind', 'Source': '/var/lib/g2/' + name + '/pgdata',
               'Target': '/var/lib/postgresql/data', 'ReadOnly': False}]
    command = ['/usr/lib/postgresql/16/bin/postgres']
    args = ['-D', '/var/lib/postgresql/data', '-c', 'listen_addresses=0.0.0.0', '-c', 'port=5432',
            '-c', 'unix_socket_directories=', '-c', 'archive_mode=off', '-c', 'archive_command=',
            '-c', 'max_wal_senders=0', '-c', 'max_replication_slots=0', '-c', 'wal_level=replica',
            '-c', 'logging_collector=off']
    if init:
        command = ['/usr/lib/postgresql/16/bin/initdb']
        args = ['-D', '/var/lib/postgresql/data', '-U', 'postgres', '--pwfile=/run/g2-postgres-secret',
                '--auth-local=reject', '--auth-host=scram-sha-256', '--encoding=UTF8', '--locale=C.UTF-8']
        mounts.append({'Type': 'bind', 'Source': '/run/g2/pg-secret-' + name,
                       'Target': '/run/g2-postgres-secret', 'ReadOnly': True})
    network, project, service = ('g2-initial-app', 'g2app', 'db') if name == 'app' else ('g2-ledgers', 'g2domains', name)
    result = {'Image': 'sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610',
              'Entrypoint': command, 'Cmd': args, 'Labels': {'com.docker.compose.project': project,
              'com.docker.compose.service': service, 'com.docker.compose.project.working_dir': '/opt/g2/compose'},
              'User': '%d:%d' % (uid, gid), 'WorkingDir': '/var/lib/postgresql/data',
              'Env': ['PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/lib/postgresql/16/bin', 'LANG=C.UTF-8'],
              'HostConfig': {'NetworkMode': 'none' if init else network, 'Dns': ['127.0.0.1'],
                             'ReadonlyRootfs': True, 'Privileged': False, 'CapDrop': ['ALL'],
                             'SecurityOpt': ['no-new-privileges'], 'PidsLimit': 256,
                             'RestartPolicy': {'Name': 'no'}, 'LogConfig': no_log, 'Mounts': mounts}}
    if not init:
        result['NetworkingConfig'] = {'EndpointsConfig': {network: {'IPAMConfig': {'IPv4Address': hosts[name]}}}}
    return result


def _replay_continuity(host, records, prefix, hashes, account, browser_image, scenario):
    principals, transactions, publications, objects = {}, {}, {}, {}
    grants, invocations, daemons, generations, active_grants = {}, {}, {}, {}, {}
    registries, registry_records = {}, {}
    held_files, grant_cleanup, nested_registry = {}, {}, {}
    internal_detaches = {}
    internal_queries = {}
    deleted_registry_values, active_delete_permits, retired_delete_permits = {}, {}, {}
    syscall_audit_positions = {}
    baseline_file = None
    enrollments, cgroup_identities, enrollment_ids = {}, {}, set()
    cgroup_root = None
    guard_records, live_ids, removed, attachments = {}, {}, {}, {}
    bootstrap_map_references = set()
    checkpoint = None
    checkpoint_failed = False
    guard_watermark, nonces, notifications = 0, set(), set()
    link_facts = {}
    fd_active, fd_stages = None, []
    prepared_sequences = {}
    denials = []
    cancellations = {}
    notification_responses = {}
    audited_transactions = set()
    audited_loads, audited_unloads = set(), set()
    fd_registry_commits = []
    extra_stages = [e for e in records if e['kind'] == 'selftest_runtime_stage' and
                    e['body']['mode'] == 'extra-fd-delays-removal' and e['body']['phase'] == 'injected']
    deferred_extra = None
    if extra_stages:
        host.require(scenario == 'bpf-authorization' and len(extra_stages) == 1,
                     'BPF deferred descriptor outside its single authorized case')
        stage = extra_stages[0]
        _verify_extra_fd_case(host, {'name': 'extra-fd-delays-removal',
                                    'actor': {'task_cookie': stage['body']['notifying_cookie']},
                                    'evidence_sequences': [e['sequence'] for e in records]}, records)
        observed = [e for e in records if e['kind'] == 'selftest_case' and
                    e['body']['name'] == 'extra-fd-delays-removal' and e['body']['phase'] == 'observed' and
                    e['body']['nonce'] == stage['body']['nonce']]
        host.require(len(observed) == 1 and stage['sequence'] < observed[0]['sequence'],
                     'BPF deferred descriptor lacks its observed case')
        deferred_extra = {'grant_id': stage['body']['grant_id'],
                          'cookie': stage['body']['mutation']['program_cookie'],
                          'stage_sequence': stage['sequence'], 'observed_sequence': observed[0]['sequence'],
                          'txids': set(observed[0]['body']['transaction_ids'])}
    replacement = None
    replacement_cases = [e for e in records if e['kind'] == 'selftest_case' and
                         e['body']['name'] == 'attachment-replace']
    if replacement_cases:
        host.require(scenario == 'bpf-authorization' and
                     [e['body']['phase'] for e in replacement_cases] == ['started', 'observed', 'settled'],
                     'BPF internal replacement outside its authorized case')
        _verify_program_cleanup_case(host, {'name': 'attachment-replace',
                                          'evidence_sequences': [e['sequence'] for e in records]}, records)
        case = replacement_cases[-1]['body']
        owners = [e['body'] for e in records if e['kind'] == 'invocation' and e['body']['id'] == case['invocation_id']]
        host.require(owners and len(case['reference_cookies']) == 2,
                     'BPF replacement case lacks its grant and distinct programs')
        replacement = {'grant_id': owners[0]['grant_id'], 'txids': set(case['transaction_ids']),
                       'cookies': case['reference_cookies'], 'start': replacement_cases[0]['sequence'],
                       'observed': replacement_cases[1]['sequence']}


    def other_holder(cookie, grant_id, current):
        obj = objects.get(cookie)
        creator = None if obj is None else transactions.get(obj['owner_transaction'])
        if (obj is None or obj['class'] != 'program' or obj['state'] != 'live' or creator is None or
                creator['grant_id'] != grant_id):
            return False
        retained_creator = (creator['state'] == 'retired' and creator['result'] is not None and creator['result'] >= 0 and
                            creator['returned_fd'] is not None and cookie not in grant_cleanup)
        retained_attachment = any(item['grant_id'] == grant_id and item['program_cookie'] == cookie and
                                  item['state'] == 'attached' for item in attachments.values())
        retained_alias = any(txn['id'] != current and txn['grant_id'] == grant_id and
                             txn['command'] == 'PROG_GET_FD_BY_ID' and txn['state'] == 'response_settled' and
                             txn['cleanup']['policy'] == 'close_borrowed_ref' and txn['cleanup']['state'] == 'pending' and
                             txn['output'] is not None and txn['output'].get('object_cookie') == cookie
                             for txn in transactions.values())
        return retained_creator or retained_attachment or retained_alias

    def settled_or_retained(txn):
        if txn['state'] == 'retired':
            return True
        output = txn['output']
        obj = None if type(output) is not dict else objects.get(output.get('object_cookie'))
        creator = None if obj is None else transactions.get(obj['owner_transaction'])
        return (txn['command'] == 'PROG_GET_FD_BY_ID' and txn['state'] == 'response_settled' and
                txn['result'] is not None and txn['result'] >= 0 and txn['returned_fd'] is not None and
                txn['id'] in audited_transactions and txn['cleanup'] == {'policy': 'close_borrowed_ref', 'state': 'pending', 'steps': []} and
                obj is not None and obj['class'] == 'program' and obj['state'] == 'live' and
                creator is not None and creator['grant_id'] == txn['grant_id'])

    for event in records:
        kind, body, seq = event['kind'], event['body'], event['sequence']
        if checkpoint_failed:
            host.require(kind not in ('runtime_exec', 'cgroup_enrollment') and
                         not (kind == 'selftest_fd_stage' and body['phase'] != 'restored') and
                         not (kind == 'held_file' and body['source'] == 'runtime_import') and
                         not (kind == 'checkpoint'), 'BPF authority resumed admission after checkpoint owner death')
            if kind == 'principal':
                host.require(body['id'] in principals, 'BPF registered a new principal after owner death')
            if kind == 'invocation':
                host.require(body['id'] in invocations and body['state'] in ('settled', 'failed'),
                             'BPF admitted a new invocation after owner death')
            if kind == 'grant':
                host.require(body['phase'] == 'revoked', 'BPF grant changed after checkpoint owner death')
            if kind == 'transaction' and body['state'] == 'committed' and body['command'] == 'PROG_QUERY':
                host.require(False, 'BPF query committed after checkpoint owner death')
            if kind == 'transaction' and body['state'] == 'committed' and body['grant_id'] is not None:
                host.require(body['command'] == 'PROG_DETACH' and body['requester_cookie'] is None and
                             body['notification_id'] is None, 'BPF new runtime effect committed after owner death')
        if kind == 'principal':
            old = principals.get(body['id'])
            if old is not None:
                host.require(all(old[key] == body[key] for key in old if key != 'terminal') and
                             (old['terminal'] is None or old['terminal'] == body['terminal']),
                             'BPF principal identity changed or was resurrected')
            principals[body['id']] = body
        elif kind == 'grant':
            owner = principals.get(body['owner'])
            old_grant = grants.get(body['id'])
            host.require(owner is not None and
                         ((owner['terminal'] is None and body['phase'] != 'revoked') or
                          (owner['terminal'] == 'exit' and body['phase'] == 'revoked' and old_grant is not None and
                           old_grant['phase'] in ('admitted', 'active', 'settling'))),
                         'BPF grant lacks a live owner or an actual owner-exit revocation')
            allowed = {'BOOT_OWNER': {'postgres-uid', 'postgres-gid', 'dns-preflight'},
                       'REALM_ROOT': {'postgres-init-' + name for name in ('app', 'w1', 'w2', 'control')} |
                                     {'postgres-live-' + name for name in ('app', 'w1', 'w2', 'control')} | {'browser-negative'},
                       'RECOVERY_OBSERVER': {'recovery-' + name for name in ('app', 'w1', 'w2', 'control')}}
            host.require(body['purpose'] in allowed.get(owner['role'], set()), 'BPF grant purpose exceeds principal authority')
            if owner['role'] == 'BOOT_OWNER':
                host.require(body['config'] == _bootstrap_template(body['purpose']),
                             'BPF bootstrap grant differs from its fixed template')
            else:
                expected = _workload_template(body['purpose'], account[0], account[1], browser_image)
                host.require(expected is not None and body['config'] == expected and
                             (body['purpose'] != 'browser-negative' or scenario == 'extra-browser'),
                             'BPF workload grant differs from its fixed template')
            old = grants.get(body['id'])
            if old is None:
                previous = active_grants.get(body['container_id'])
                host.require(body['phase'] == 'admitted' and (previous is None or grants[previous]['phase'] == 'retired') and
                             body['invocation'] == generations.get(body['container_id'], 0) + 1,
                             'BPF grant container generation is stale or overlaps')
                generations[body['container_id']] = body['invocation']
                active_grants[body['container_id']] = body['id']
            else:
                allowed_next = {'admitted': ('admitted', 'active', 'settling', 'revoked'), 'active': ('active', 'settling', 'revoked'),
                                'settling': ('settling', 'retired', 'revoked'), 'retired': ('retired',), 'revoked': ()}
                host.require(all(old[field] == body[field] for field in old if field != 'phase') and
                             body['phase'] in allowed_next[old['phase']], 'BPF grant changed or moved backwards')
                if old['phase'] == 'admitted' and body['phase'] == 'settling':
                    failed = [inv for inv in invocations.values() if inv['grant_id'] == body['id'] and
                              inv['command'] == 'create' and inv['state'] == 'failed' and
                              inv['generation'] == body['invocation'] and inv['config_sha256'] == body['config_sha256']]
                    host.require(len(failed) == 1, 'BPF admitted grant settled without its failed create invocation')
                if body['phase'] == 'retired' and body['id'] in enrollments:
                    group = enrollments[body['id']]
                    host.require(group['used'] and group['release_seq'] is not None and
                                 all(value['grant_id'] != body['id'] or name == 'realm_targets' and value['phase'] == 4
                                     for (name, _), value in registries.items() if name not in ('broker_token', 'guard_liveness')),
                                 'BPF grant retired before its enrolled cgroup and registry settled')
                if body['phase'] == 'retired':
                    related = {ident for ident, txn in transactions.items() if txn['grant_id'] == body['id']}
                    host.require(all(transactions[ident]['state'] == 'retired' and
                                     (transactions[ident]['result'] is None or ident in audited_transactions) for ident in related) and
                                 all(obj['removal_seq'] is not None and obj['cookie'] in removed and
                                     removed[obj['cookie']]['sequence'] == obj['removal_seq']
                                     for obj in objects.values() if obj['owner_transaction'] in related) and
                                 all(item['state'] == 'released' for item in attachments.values() if item['grant_id'] == body['id']) and
                                 all(value['grant_id'] != body['id'] or name == 'realm_targets' and value['phase'] == 4
                                     for (name, _), value in registries.items() if name not in ('broker_token', 'guard_liveness')),
                                 'BPF grant retired with unresolved effects')
                    host.require(all(transactions[obj['owner_transaction']]['result'] < 0 or
                                     transactions[obj['owner_transaction']]['returned_fd'] is None or
                                     grant_cleanup.get(obj['cookie'], {}).get('step') == 2
                                     for obj in objects.values() if obj['owner_transaction'] in related),
                                 'BPF grant retired before its retained object cleanup completed')
                    host.require(all(obj['class'] != 'program' or
                                     ((transactions[obj['owner_transaction']]['result'] < 0 or obj['cookie'] in audited_loads) and
                                      (obj['cookie'] not in audited_loads or obj['cookie'] in audited_unloads))
                                     for obj in objects.values() if obj['owner_transaction'] in related),
                                 'BPF grant retired before its program unload audit settled')
            grants[body['id']] = body
        elif kind == 'daemon':
            old = daemons.get(body['cookie'])
            host.require(body['owner'] in principals, 'BPF daemon owner missing')
            _daemon_transition(host, old, body)
            daemons[body['cookie']] = body
        elif kind == 'invocation':
            principal, daemon = principals.get(body['principal_id']), daemons.get(body['daemon_cookie'])
            host.require(principal is not None and principal['role'] == 'RUNTIME' and
                         principal['task_cookie'] == body['task_cookie'] and daemon is not None,
                         'BPF invocation principal or daemon missing')
            if body['command'] != 'features':
                grant = grants.get(body['grant_id'])
                host.require(grant is not None and (grant['phase'] in ('admitted', 'active', 'settling') or
                             grant['phase'] == 'revoked' and body['id'] in invocations and body['state'] in ('settled', 'failed')) and
                             grant['container_id'] == body['container_id'] and grant['invocation'] == body['generation'] and
                             grant['config_sha256'] == body['config_sha256'], 'BPF invocation grant or generation differs')
            old = invocations.get(body['id'])
            if old is None:
                host.require(body['state'] == 'bound', 'BPF invocation was not first bound')
            else:
                transitions = {'bound': ('bound', 'running', 'failed'), 'running': ('running', 'settled', 'failed'),
                               'settled': ('settled',), 'failed': ('failed',)}
                host.require(all(old[field] == body[field] for field in old if field != 'state') and
                             body['state'] in transitions[old['state']], 'BPF invocation changed or moved backwards')
                if body['state'] in ('settled', 'failed') and old['state'] != body['state']:
                    related = [txn for txn in transactions.values() if txn['invocation_id'] == body['id']]
                    host.require(all(txn['state'] == 'retired' and
                                     (txn['result'] is None or txn['id'] in audited_transactions) and
                                     txn['cleanup']['state'] in ('not_needed', 'settled') for txn in related),
                                 'BPF runtime terminated before notification, audit and cleanup settled')
            invocations[body['id']] = body
        elif kind == 'held_file':
            host.require(body['cookie'] == seq and body['cookie'] not in held_files,
                         'BPF held file is not its unique transcript identity')
            if body['source'] == 'bootstrap':
                host.require(baseline_file is None, 'BPF held device baseline duplicated')
                baseline_file = body
            else:
                invocation = invocations.get(body['invocation_id'])
                host.require(baseline_file is not None and body['baseline_cookie'] == baseline_file['cookie'] and
                             all(body[key] == baseline_file[key] for key in ('device', 'inode', 'rdev', 'mode', 'uid', 'gid')) and
                             invocation is not None and invocation['state'] in ('bound', 'running') and
                             invocation['listener_id'] is not None and
                             (body['invocation_id'], body['notification_id']) not in notifications,
                             'BPF imported device lacks its original request and baseline node')
                host.require(invocation['grant_id'] in grants and grants[invocation['grant_id']]['phase'] != 'revoked',
                             'BPF revoked grant imported a new descriptor')
            held_files[body['cookie']] = {'record': body, 'used': False}
        elif kind == 'cgroup_enrollment':
            grant = grants.get(body['grant_id'])
            invocation = invocations.get(body['invocation_id'])
            identity = (body['root_cookie'], body['cgroup_id'])
            host.require(grant is not None and grant['phase'] in ('admitted', 'active') and
                         invocation is not None and invocation['grant_id'] == body['grant_id'] and
                         invocation['state'] in ('bound', 'running') and invocation['listener_id'] is not None and
                         body['grant_id'] not in enrollments and body['enrollment'] not in enrollment_ids and
                         identity not in cgroup_identities and (cgroup_root is None or cgroup_root == body['root_cookie']),
                         'BPF cgroup enrollment lacks a unique live grant and invocation')
            cgroup_root = body['root_cookie']
            enrollment_ids.add(body['enrollment'])
            cgroup_identities[identity] = body['grant_id']
            enrollments[body['grant_id']] = {'record': body, 'sequence': seq, 'used': False, 'offline_seq': None, 'release_seq': None}
        elif kind == 'guard':
            item = body['record']
            guard_watermark = item['sequence']
            guard_records[item['sequence']] = item
            if item['kind'] in (5, 6):
                grant_id = cgroup_identities.get((item['root_cookie'], item['cgroup_id']))
                if grant_id is not None:
                    group = enrollments[grant_id]
                    key = 'offline_seq' if item['kind'] == 5 else 'release_seq'
                    host.require(group[key] is None and (item['kind'] != 5 or group['release_seq'] is None) and
                                 (item['kind'] != 6 or group['offline_seq'] is not None),
                                 'BPF cgroup lifecycle event duplicated or reordered')
                    group[key] = item['sequence']
            if item['kind'] in (1, 3):
                identity = (item['kind'], item['kernel_id'])
                host.require(item['object_cookie'] > 0 and item['kernel_id'] > 0 and
                             item['object_cookie'] not in publications and identity not in live_ids,
                             'BPF object publication reused a live identity')
                publications[item['object_cookie']] = item
                live_ids[identity] = item['object_cookie']
            elif item['kind'] in (2, 4):
                publication = publications.get(item['object_cookie'])
                host.require(publication is not None and publication['kind'] == item['kind'] - 1 and
                             publication['kernel_id'] == item['kernel_id'] and
                             live_ids.get((publication['kind'], item['kernel_id'])) == item['object_cookie'] and
                             item['object_cookie'] not in removed, 'BPF object removal has no live incarnation')
                removed[item['object_cookie']] = item
                del live_ids[(publication['kind'], item['kernel_id'])]
        elif kind == 'object':
            publication = publications.get(body['cookie'])
            host.require(publication is not None and publication['kind'] == (1 if body['class'] == 'program' else 3) and
                         publication['kernel_id'] == body['kernel_id'] and publication['sequence'] == body['publication_seq'] and
                         publication['object_snapshot'] == {'version': 1, 'type': body['type'], 'attributes': body['attributes']} and
                         body['attribute_source'] == {'kind': 'native_publication', 'guard_sequence': publication['sequence']},
                         'BPF object lacks matching prior guard snapshot')
            old = objects.get(body['cookie'])
            if old is not None:
                host.require(all(old[key] == body[key] for key in old if key not in ('state', 'removal_seq')) and
                             (old['removal_seq'] is None or old['removal_seq'] == body['removal_seq']),
                             'BPF object incarnation changed')
            if body['removal_seq'] is not None:
                removal = removed.get(body['cookie'])
                host.require(removal is not None and removal['sequence'] == body['removal_seq'],
                             'BPF object removal lacks matching guard event')
            if old is not None:
                transitions = {'live': ('live', 'id_removed', 'reclaim_pending'),
                               'id_removed': ('id_removed', 'reclaim_pending'), 'reclaim_pending': ('reclaim_pending',)}
                host.require(body['state'] in transitions[old['state']], 'BPF object incarnation moved backwards')
            objects[body['cookie']] = body
        elif kind == 'transaction':
            ident = body['id']
            internal_detach = (body['grant_id'] is not None and body['command'] == 'PROG_DETACH' and
                               all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
            internal_replacement = (body['grant_id'] is not None and body['command'] in ('PROG_LOAD', 'PROG_ATTACH') and
                                    all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
            old = transactions.get(ident)
            states = ('prepared', 'committed', 'effect_observed', 'response_settled', 'retired')
            notification = (body['invocation_id'], body['notification_id'])
            cancelled_retirement = (old is not None and old['state'] == 'prepared' and body['state'] == 'retired' and
                                    notification in cancellations and cancellations[notification]['transaction_id'] == ident)
            broker = principals.get(prefix['broker_principal'])
            host.require(broker is not None and broker['role'] == 'BOOTSTRAP_BROKER' and
                         broker['task_cookie'] == body['executor_cookie'] and broker['terminal'] is None,
                         'BPF transaction executor is not the original broker task')
            if old is None:
                host.require(body['state'] == 'prepared' and body['result'] is None and body['output'] is None and
                             body['returned_fd'] is None, 'BPF transaction did not begin prepared')
                prepared_sequences[ident] = seq
            else:
                host.require((cancelled_retirement or states.index(body['state']) == states.index(old['state']) + 1) and
                             all(old[key] == body[key] for key in old if key not in ('state', 'result', 'returned_fd', 'output', 'cleanup')),
                             'BPF transaction changed identity or skipped a phase')
                if cancelled_retirement:
                    host.require(all(body[key] is None for key in ('result', 'returned_fd', 'output')) and
                                 old['cleanup'] == body['cleanup'] == {'policy': 'none', 'state': 'not_needed', 'steps': []},
                                 'BPF unexecuted cancellation claimed an effect or cleanup')
                if old['state'] in ('effect_observed', 'response_settled'):
                    host.require(old['result'] == body['result'] and old['output'] == body['output'],
                                 'BPF kernel effect changed after observation')
                if old['state'] == 'response_settled':
                    host.require(old['returned_fd'] == body['returned_fd'], 'BPF settled descriptor response changed')
            if body['state'] in ('response_settled', 'retired') and body['invocation_id'] is not None and body['notification_id'] is not None and body['result'] is not None:
                delivery = notification_responses.get(ident)
                host.require(delivery is not None and body['returned_fd'] == delivery['returned_fd'],
                             'BPF runtime transaction lacks its actual response ioctl')
            if body['state'] == 'effect_observed' and ident in nested_registry:
                for registry_seq in nested_registry[ident]:
                    registry = registry_records[registry_seq]
                    control = transactions.get(registry['transaction_id'])
                    lookups = [txn for txn in transactions.values() if txn['command'] == 'MAP_LOOKUP_ELEM' and
                               txn['state'] == 'retired' and txn['result'] == 0 and
                               type(txn['output']) is dict and txn['output'].get('registry_sequence') == registry_seq]
                    host.require(control is not None and control['state'] == 'retired' and control['result'] == 0 and
                                 (registry['action'] == 'delete' or bool(lookups)),
                                 'BPF parent effect preceded its nested control and readback settlement')
            if body['command'] == 'PROG_GET_FD_BY_ID' and body['state'] in ('response_settled', 'retired') and body['result'] is not None and body['result'] >= 0:
                output = body['output']
                delivery = notification_responses.get(ident)
                host.require(_shape(output, ('object_cookie',)) and delivery is not None and
                             (body['returned_fd'] is not None or delivery['ioctl_errno'] == 2 or
                              delivery['operation'] == 'SEND' and delivery['response']['error'] < 0) and
                             body['cleanup']['policy'] == 'close_borrowed_ref' and
                             body['cleanup']['state'] == ('pending' if body['state'] == 'response_settled' else 'settled'),
                             'BPF borrowed descriptor lacks a known transferred response and cleanup')
                if body['state'] == 'retired':
                    host.require(body['cleanup']['steps'][0]['object_cookie'] == output['object_cookie'] and
                                 other_holder(output['object_cookie'], body['grant_id'], ident),
                                 'BPF borrowed close lacks another proven live holder')
            if body['state'] == 'committed':
                host.require(body['result'] is None and body['output'] is None and body['returned_fd'] is None,
                             'BPF transaction claimed an effect before execution')
                if body['grant_id'] is not None:
                    host.require(body['grant_id'] in grants and (grants[body['grant_id']]['phase'] != 'revoked' or internal_detach),
                                 'BPF revoked grant committed a new runtime effect')
                if fd_active is not None:
                    if body['grant_id'] is None:
                        host.require(body['command'] in ('MAP_LOOKUP_ELEM', 'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'OBJ_GET_INFO_BY_FD'),
                                     'BPF unrelated bootstrap effect committed during FD selftest')
                        fd_registry_commits.append((ident, fd_active['grant_id']))
                    else:
                        host.require(body['grant_id'] == fd_active['grant_id'],
                                     'BPF unrelated effect committed during FD selftest')
            if body['state'] in ('effect_observed', 'response_settled', 'retired') and not cancelled_retirement:
                host.require(body['result'] is not None, 'BPF transaction actual result missing')
            if body['grant_id'] is None:
                host.require(all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')),
                             'BPF bootstrap transaction has runtime authority')
                if body['command'] == 'PROG_QUERY':
                    host.require(body['attributes']['attr_size'] == 40 and
                                 struct.unpack_from('<I', bytes.fromhex(body['attributes']['attr_hex']), 24)[0] == 64 and
                                 body['returned_fd'] is None and
                                 body['cleanup'] == {'policy': 'none', 'state': 'not_needed', 'steps': []},
                                 'BPF internal query fixed inputs differ')
                    if body['state'] == 'prepared':
                        target = body['input_objects'][0]
                        matches = [item for item in attachments.values() if item['enrollment'] == target['cookie'] and
                                   item['state'] == 'attached' and item['id'] not in internal_queries.values() and
                                   transactions[item['transaction_id']]['state'] == 'effect_observed' and
                                   transactions[item['transaction_id']]['result'] == 0 and
                                   transactions[item['transaction_id']]['output'] == {'attachment_id': item['id']}]
                        host.require(len(matches) == 1, 'BPF internal query lacks a unique pending attachment')
                        attached = matches[0]
                        group = enrollments.get(attached['grant_id'])
                        host.require(group is not None and group['used'] and group['offline_seq'] is None and
                                     group['release_seq'] is None and group['record']['enrollment'] == target['cookie'],
                                     'BPF internal query lacks its retained enrolled cgroup')
                        internal_queries[ident] = attached['id']
                    attached = attachments.get(internal_queries.get(ident))
                    host.require(attached is not None, 'BPF internal query changed its attachment binding')
                    if body['state'] in ('effect_observed', 'response_settled', 'retired'):
                        program = objects.get(attached['program_cookie'])
                        host.require(program is not None and program['state'] == 'live' and body['result'] == 0 and
                                     body['output'] == {'attach_flags': 2, 'program_ids': [program['kernel_id']],
                                                        'program_attach_flags': [2]},
                                     'BPF internal query did not confirm the actual attached program')
            elif internal_detach:
                grant = grants.get(body['grant_id'])
                if body['state'] == 'prepared':
                    target, program = body['input_objects']
                    matches = [item for item in attachments.values() if item['grant_id'] == body['grant_id'] and
                               item['enrollment'] == target['cookie'] and item['program_cookie'] == program['cookie'] and
                               item['state'] == 'attached']
                    host.require(grant is not None and len(matches) == 1, 'BPF internal detach lacks a unique owned attachment')
                    attached = matches[0]
                    original = transactions.get(attached['transaction_id'])
                    response = None if original is None else notification_responses.get(original['id'])
                    pending_cleanup = {'policy': 'detach_then_close', 'state': 'pending', 'steps': [
                        {'sequence': 1, 'action': 'detach', 'object_cookie': attached['program_cookie'],
                         'attachment_id': attached['id'], 'guard_sequence': None, 'result': None}]}
                    test_extra = (deferred_extra is not None and deferred_extra['grant_id'] == body['grant_id'] and
                                  deferred_extra['cookie'] == program['cookie'] and ident in deferred_extra['txids'] and
                                  deferred_extra['stage_sequence'] < seq)
                    test_replacement = (replacement is not None and replacement['grant_id'] == body['grant_id'] and
                                        replacement['cookies'][1] == program['cookie'] and ident in replacement['txids'] and
                                        replacement['observed'] < seq)
                    host.require(original is not None and original['command'] == 'PROG_ATTACH' and
                                 original['grant_id'] == body['grant_id'] and original['result'] == 0 and
                                 ((original['state'] == 'response_settled' and original['cleanup'] == pending_cleanup and
                                   response is not None and response['ioctl_errno'] == 2) or
                                  ((test_extra or test_replacement) and original['state'] == 'retired')),
                                 'BPF internal detach lacks its original preauthorized attachment')
                    internal_detaches[ident] = attached['id']
                attachment_id = internal_detaches.get(ident)
                host.require(attachment_id is not None and body['cleanup'] == {'policy': 'none', 'state': 'not_needed', 'steps': []},
                             'BPF internal detach changed its cleanup binding')
                if body['state'] in ('effect_observed', 'response_settled', 'retired'):
                    attached = attachments.get(attachment_id)
                    host.require(_shape(body['output'], ('attachment_id',)) and
                                 (body['result'] == 0 and body['output']['attachment_id'] == attachment_id and
                                  attached is not None and attached['state'] in ('detached', 'released') or
                                  body['result'] < 0 and body['output']['attachment_id'] is None),
                                 'BPF internal detach result differs from actual attachment state')
                if body['state'] == 'retired':
                    host.require(ident in audited_transactions, 'BPF internal detach retired before syscall audit')
            elif internal_replacement:
                host.require(replacement is not None and replacement['grant_id'] == body['grant_id'] and
                             ident in replacement['txids'] and replacement['start'] < prepared_sequences[ident] < replacement['observed'] and
                             body['returned_fd'] is None and body['cleanup'] == {'policy': 'none', 'state': 'not_needed', 'steps': []},
                             'BPF broker replacement effect lacks its authorized case')
                if body['command'] == 'PROG_LOAD':
                    host.require(body['program_bytes'] == _device_program().hex() and body['license'] == 'Apache' and
                                 not body['input_objects'], 'BPF broker replacement program differs')
                    if body['output'] is not None:
                        host.require(body['output']['object_cookie'] == replacement['cookies'][1],
                                     'BPF replacement load published another program')
                else:
                    inputs = body['input_objects']
                    host.require(len(inputs) == 3 and inputs[1]['cookie'] == replacement['cookies'][1] and
                                 inputs[2]['cookie'] == replacement['cookies'][0] and
                                 struct.unpack_from('<I', bytes.fromhex(body['attributes']['attr_hex']), 12)[0] == 6,
                                 'BPF broker replacement attach differs from its case programs')
            else:
                host.require(all(_integer(body[key], 1) for key in ('invocation_id', 'requester_cookie', 'notification_id')),
                             'BPF runtime transaction lacks notification binding')
                invocation = invocations.get(body['invocation_id'])
                host.require(invocation is not None and invocation['grant_id'] == body['grant_id'] and
                             invocation['state'] in ('bound', 'running') and
                             invocation['command'] in ('create', 'delete') and
                             body['command'] in ('MAP_CREATE', 'PROG_LOAD', 'PROG_QUERY', 'PROG_GET_FD_BY_ID',
                                                 'OBJ_GET_INFO_BY_FD', 'PROG_ATTACH', 'PROG_DETACH') and
                             (invocation['command'] != 'delete' or body['command'] == 'PROG_DETACH'),
                             'BPF runtime notification lacks its original invocation authority')
                if old is None:
                    grant = grants.get(body['grant_id'])
                    host.require(grant is not None and grant['phase'] in ('admitted', 'active', 'settling'),
                                 'BPF retired grant admitted a new notification')
                    notification = (body['invocation_id'], body['notification_id'])
                    host.require(notification not in notifications, 'BPF notification was executed twice')
                    notifications.add(notification)
                if body['state'] == 'prepared':
                    for item in body['input_objects']:
                        if item['kind'] in ('program', 'map'):
                            obj = objects.get(item['cookie'])
                            owner = None if obj is None else transactions.get(obj['owner_transaction'])
                            host.require(obj is not None and obj['class'] == item['kind'] and obj['state'] == 'live' and
                                         obj['cookie'] not in grant_cleanup and owner is not None and owner['grant_id'] == body['grant_id'],
                                         'BPF transaction imported another grant object')
                        elif item['kind'] == 'dev_null':
                            held = held_files.get(item['cookie'])
                            host.require(held is not None and not held['used'] and held['record']['source'] == 'runtime_import' and
                                         held['record']['requester_fd'] == item['requester_fd'] and
                                         all(held['record'][key] == body[key] for key in
                                             ('invocation_id', 'requester_cookie', 'notification_id')),
                                         'BPF input does not use the actual imported requester device')
                            held['used'] = True
                        elif item['kind'] == 'cgroup':
                            group = enrollments.get(body['grant_id'])
                            host.require(group is not None and group['release_seq'] is None and group['offline_seq'] is None and
                                         group['record']['enrollment'] == item['cookie'],
                                         'BPF transaction target lacks prior cgroup enrollment')
                            if not group['used']:
                                host.require(all(group['record'][key] == body[key] for key in
                                                 ('grant_id', 'invocation_id', 'requester_cookie', 'notification_id')),
                                             'BPF first cgroup input differs from its enrollment request')
                                host.require(not any(group['sequence'] < prior_seq < seq and prior['grant_id'] == body['grant_id']
                                                     for prior_seq, prior in registry_records.items()),
                                             'BPF initial enrollment acquired registry effects before preparation')
                                group['used'] = True
                if body['state'] == 'retired' and not cancelled_retirement:
                    output, result, command = body['output'], body['result'], body['command']
                    delivery = notification_responses.get(ident)
                    if (delivery is not None and output is not None and
                            (delivery['ioctl_errno'] == 2 or delivery['operation'] == 'SEND' and delivery['response']['error'] < 0)):
                        if command in ('MAP_CREATE', 'PROG_LOAD') and output.get('object_cookie') is not None:
                            obj = objects.get(output['object_cookie'])
                            host.require(obj is not None and obj['owner_transaction'] == ident and
                                         obj['removal_seq'] is not None and obj['cookie'] in removed and
                                         body['cleanup']['state'] == 'settled',
                                         'BPF undelivered creation retired without actual object cleanup')
                        if command == 'PROG_ATTACH' and output.get('attachment_id') is not None:
                            attachment = attachments.get(output['attachment_id'])
                            expected_cleanup = {'policy': 'detach_then_close', 'state': 'settled', 'steps': [
                                {'sequence': 1, 'action': 'detach', 'object_cookie': attachment['program_cookie'],
                                 'attachment_id': attachment['id'], 'guard_sequence': None, 'result': 0}]} if attachment is not None else None
                            host.require(attachment is not None and attachment['grant_id'] == body['grant_id'] and
                                         attachment['state'] in ('detached', 'released') and body['cleanup'] == expected_cleanup and
                                         any(txn['command'] == 'PROG_DETACH' and txn['grant_id'] == body['grant_id'] and
                                             txn['state'] == 'retired' and txn['result'] == 0 and txn['id'] in audited_transactions and
                                             internal_detaches.get(txn['id']) == attachment['id'] and
                                             txn['output'] == {'attachment_id': attachment['id']} for txn in transactions.values()),
                                         'BPF undelivered attach retired without its actual audited detach')
                    if output is not None and output.get('object_cookie') is not None:
                        obj = objects.get(output['object_cookie'])
                        host.require(obj is not None, 'BPF transaction output names an unknown object')
                        if command in ('MAP_CREATE', 'PROG_LOAD'):
                            host.require(obj['owner_transaction'] == ident and
                                         obj['class'] == ('map' if command == 'MAP_CREATE' else 'program') and
                                         (command != 'PROG_LOAD' or obj['bytes'] == body['program_bytes']),
                                         'BPF created object differs from its actual transaction')
                        elif command == 'OBJ_GET_INFO_BY_FD':
                            host.require(output['attributes'] == obj['attributes'] and output['type'] == obj['type'] and
                                         output['kernel_id'] == obj['kernel_id'] and
                                         body['input_objects'][0]['kind'] == obj['class'], 'BPF object information changed')
                        elif command == 'PROG_GET_FD_BY_ID':
                            requested = struct.unpack_from('<I', bytes.fromhex(body['attributes']['attr_hex']))[0]
                            owner = transactions.get(obj['owner_transaction'])
                            host.require(obj['class'] == 'program' and obj['kernel_id'] == requested and
                                         owner is not None and owner['grant_id'] == body['grant_id'],
                                         'BPF program ID lookup adopted another grant object')
                    if command == 'PROG_QUERY' and result >= 0:
                        for kernel_id in output['program_ids']:
                            matches = [obj for obj in objects.values() if obj['class'] == 'program' and
                                       obj['kernel_id'] == kernel_id and obj['state'] == 'live']
                            host.require(len(matches) == 1 and
                                         transactions[matches[0]['owner_transaction']]['grant_id'] == body['grant_id'],
                                         'BPF query returned an unowned program')
                    if command == 'PROG_LOAD' and body['license'] == 'MIT' and result >= 0:
                        raw_attr = bytes.fromhex(body['attributes']['attr_hex']).ljust(4096, b'\0')
                        host.require(struct.unpack_from('<I', raw_attr)[0] != 1,
                                     'BPF invalid extended information probe unexpectedly succeeded')
                    if command == 'PROG_LOAD' and body['license'] == 'MIT' and output is not None:
                        raw_attr = bytes.fromhex(body['attributes']['attr_hex']).ljust(4096, b'\0')
                        if struct.unpack_from('<I', raw_attr)[0] == 1:
                            host.require(result == -9 and output['object_cookie'] is None,
                                         'BPF extended information probe did not return EBADF')
                    if command == 'PROG_ATTACH' and body['input_objects'][0]['kind'] == 'dev_null':
                        host.require(result == -9 and (output is None or output['attachment_id'] is None),
                                     'BPF replacement probe did not return EBADF')
                    cleanup = body['cleanup']
                    steps = cleanup['steps']
                    host.require(cleanup['state'] in ('not_needed', 'settled') and
                                 (cleanup['policy'] != 'none' or cleanup['state'] == 'not_needed' and not steps) and
                                 (cleanup['state'] != 'not_needed' or cleanup['policy'] == 'none'),
                                 'BPF retired transaction has unresolved cleanup')
                    host.require(all(steps[index]['sequence'] < steps[index + 1]['sequence']
                                     for index in range(len(steps) - 1)), 'BPF cleanup steps are not ordered')
                    for step in steps:
                        if step['object_cookie'] is not None:
                            obj = objects.get(step['object_cookie'])
                            owner = None if obj is None else transactions.get(obj['owner_transaction'])
                            host.require(owner is not None and owner['grant_id'] == body['grant_id'],
                                         'BPF cleanup names another grant object')
                        if step['attachment_id'] is not None:
                            attachment = attachments.get(step['attachment_id'])
                            host.require(attachment is not None and attachment['grant_id'] == body['grant_id'],
                                         'BPF cleanup names another grant attachment')
                        if step['guard_sequence'] is not None:
                            guard = guard_records.get(step['guard_sequence'])
                            host.require(guard is not None and
                                         (step['action'] != 'await_id_removed' or guard['kind'] in (2, 4) and
                                          guard['object_cookie'] == step['object_cookie']) and
                                         (step['action'] != 'await_cgroup_release' or guard['kind'] == 6),
                                         'BPF cleanup guard observation differs')
                    if cleanup['policy'] == 'close_unattached' and cleanup['state'] == 'settled':
                        host.require(any(step['action'] == 'await_id_removed' and step['guard_sequence'] is not None
                                         for step in steps), 'BPF closed object has no observed ID removal')
                    if cleanup['policy'] == 'auto_empty' and cleanup['state'] == 'settled':
                        host.require(any(step['action'] == 'await_cgroup_release' and step['guard_sequence'] is not None
                                         for step in steps), 'BPF automatic retirement lacks release observation')
            if body['grant_id'] is None and body['command'] == 'PROG_LOAD' and body['state'] == 'prepared':
                program = bytes.fromhex(body['program_bytes']) if body['program_bytes'] is not None else b''
                host.require(program and len(program) % 8 == 0, 'BPF bootstrap program bytes missing')
                expected_inputs = []
                index = 0
                while index < len(program):
                    if program[index] == 0x18:
                        host.require(index + 16 <= len(program), 'BPF bootstrap double instruction truncated')
                        pseudo = program[index + 1] >> 4
                        host.require(pseudo in (0, 1), 'BPF bootstrap unknown pseudo relocation')
                        if pseudo == 1:
                            host.require(program[index + 2:index + 4] == b'\0\0' and
                                         program[index + 8:index + 16] == bytes(8), 'BPF bootstrap map relocation malformed')
                            expected_inputs.append((index, struct.unpack_from('<I', program, index + 4)[0]))
                        index += 16
                    else:
                        index += 8
                host.require(len(expected_inputs) == len(body['input_objects']), 'BPF bootstrap map relocation count differs')
                for (offset, fd), item in zip(expected_inputs, body['input_objects']):
                    publication = publications.get(item['cookie'])
                    host.require(item['field'] == 'insns[%d].map_fd' % offset and item['kind'] == 'map' and
                                 item['requester_fd'] == fd and publication is not None and publication['kind'] == 3,
                                 'BPF bootstrap relocation does not use a previously published map')
                    bootstrap_map_references.add(item['cookie'])
            if body['grant_id'] is None and body['command'] == 'BTF_LOAD' and body['output'] is not None:
                output = body['output']
                host.require(_shape(output, ('btf_bytes', 'btf_sha256')) and type(output['btf_bytes']) is str and
                             re.fullmatch(r'(?:[0-9a-f]{2})+', output['btf_bytes']) is not None and
                             _hex(output['btf_sha256']) and
                             hashlib.sha256(bytes.fromhex(output['btf_bytes'])).hexdigest() == output['btf_sha256'],
                             'BPF bootstrap BTF output bytes differ')
            if body['grant_id'] is None and body['command'] in ('MAP_LOOKUP_ELEM', 'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'MAP_FREEZE') and body['output'] is not None:
                output = body['output']
                fields = ('registry_sequence', 'value_hex') if body['command'] == 'MAP_LOOKUP_ELEM' else ('registry_sequence',)
                host.require(_shape(output, fields) and _integer(output['registry_sequence'], 1) and
                             output['registry_sequence'] in registry_records and len(body['input_objects']) == 1 and
                             body['input_objects'][0]['kind'] == 'map' and
                             (body['result'] == 0 or body['command'] == 'MAP_LOOKUP_ELEM' and
                              body['result'] == -2 and output['value_hex'] is None),
                             'BPF control map readback is not recorded')
                item = objects.get(body['input_objects'][0]['cookie'])
                recorded = registry_records[output['registry_sequence']]
                host.require(item is not None and item['class'] == 'map' and item['attributes']['name'] == recorded['map'] and
                             (body['command'] in ('MAP_LOOKUP_ELEM', 'MAP_FREEZE') or recorded['transaction_id'] == ident),
                             'BPF control map output points to another registry operation')
                if body['command'] == 'MAP_LOOKUP_ELEM':
                    value = recorded['value']
                    if body['result'] == -2:
                        deleted = deleted_registry_values.get(output['registry_sequence'])
                        host.require(recorded['action'] == 'delete' and recorded['map'] == 'retire_tasks' and
                                     value is None and deleted is not None and
                                     deleted['grant_id'] == recorded['grant_id'] and
                                     (recorded['map'], _canonical(recorded['key'])) not in registries,
                                     'BPF absent lookup does not identify a current exact registry deletion')
                    else:
                        host.require(value is not None, 'BPF lookup read back a deleted registry value')
                        if recorded['map'] == 'broker_token':
                            expected = struct.pack('<Q', value['cookie'])
                        elif recorded['map'] == 'guard_liveness':
                            expected = struct.pack('<Q', value['phase'])
                        elif recorded['map'] == 'realm_targets':
                            expected = struct.pack('<QQII', value['enrollment'], value['grant_id'], value['phase'], value['retirement'])
                        else:
                            expected = struct.pack('<QQQQQ', value['enrollment'], value['grant_id'], value['invocation_id'],
                                                   value['exe_dev'], value['exe_ino'])
                        host.require(type(output['value_hex']) is str and output['value_hex'] == expected.hex(),
                                     'BPF actual registry lookup bytes differ from the recorded value')
            if body['grant_id'] is None and body['command'] == 'LINK_CREATE' and body['state'] == 'retired':
                output = body['output']
                host.require(_shape(output, ('hook', 'link_id', 'program_cookie')) and body['result'] >= 0 and
                             output['hook'] not in link_facts, 'BPF bootstrap link transaction differs')
                link_facts[output['hook']] = output
            transactions[ident] = body
            if (fd_active is not None and fd_active['phase'] == 'restored' and
                    fd_active['transaction_id'] == ident and body['state'] == 'retired'):
                fd_active = None
        elif kind == 'attachment':
            old = attachments.get(body['id'])
            program = objects.get(body['program_cookie'])
            transaction = transactions.get(body['transaction_id'])
            registry = registries.get(('realm_targets', _canonical({'root_kn_id': body['root_cookie'], 'kn_id': body['cgroup_id']})))
            host.require(program is not None and program['class'] == 'program' and transaction is not None and
                         transaction['grant_id'] == body['grant_id'] and transaction['command'] == 'PROG_ATTACH' and
                         registry is not None and registry['enrollment'] == body['enrollment'] and
                         registry['grant_id'] == body['grant_id'] and body['attach_type'] == 6 and
                         body['flags'] in (2, 6), 'BPF attachment lacks its enrolled target or transaction')
            inputs = transaction['input_objects']
            host.require(len(inputs) >= 2 and inputs[0]['kind'] == 'cgroup' and
                         inputs[0]['cookie'] == body['enrollment'] and inputs[1]['kind'] == 'program' and
                         inputs[1]['cookie'] == body['program_cookie'] and program['bytes'] == _device_program().hex() and
                         struct.unpack_from('<I', bytes.fromhex(transaction['attributes']['attr_hex']), 12)[0] == body['flags'],
                         'BPF attachment differs from its approved target and program')
            if old is None:
                host.require(body['state'] == 'attached' and body['offline_seq'] is None and body['release_seq'] is None,
                             'BPF attachment was not first observed attached')
                if body['replaces'] is not None:
                    replaced = attachments.get(body['replaces'])
                    host.require(body['flags'] == 6 and replaced is not None and replaced['grant_id'] == body['grant_id'] and
                                 replaced['enrollment'] == body['enrollment'] and replaced['state'] == 'attached',
                                 'BPF replacement does not reference the original attachment')
                    host.require(len(inputs) == 3 and inputs[2]['cookie'] == replaced['program_cookie'],
                                 'BPF replacement used a different original program')
                else:
                    host.require(body['flags'] == 2, 'BPF replacement attachment lacks predecessor')
            else:
                transitions = {'attached': ('attached', 'detached', 'offlining'), 'detached': ('detached', 'offlining', 'released'),
                               'offlining': ('offlining', 'released'), 'released': ('released',)}
                host.require(all(old[key] == body[key] for key in old if key not in ('state', 'offline_seq', 'release_seq')) and
                             body['state'] in transitions[old['state']] and
                             all(old[key] is None or old[key] == body[key] for key in ('offline_seq', 'release_seq')),
                             'BPF attachment identity or state changed')
            for field, expected_kind in (('offline_seq', 5), ('release_seq', 6)):
                if body[field] is not None:
                    observation = guard_records.get(body[field])
                    host.require(observation is not None and observation['kind'] == expected_kind and
                                 observation['root_cookie'] == body['root_cookie'] and
                                 observation['cgroup_id'] == body['cgroup_id'], 'BPF cgroup retirement event differs')
            host.require(body['state'] != 'released' or body['release_seq'] is not None,
                         'BPF attachment released without callback completion')
            attachments[body['id']] = body
        elif kind == 'grant_object_cleanup':
            obj = objects.get(body['object_cookie'])
            creator = transactions.get(body['creator_transaction_id'])
            grant = grants.get(body['grant_id'])
            prior = grant_cleanup.get(body['object_cookie'])
            extra_match = (deferred_extra is not None and deferred_extra['grant_id'] == body['grant_id'] and
                           deferred_extra['cookie'] == body['object_cookie'] and deferred_extra['stage_sequence'] < seq)
            replacement_match = (replacement is not None and replacement['grant_id'] == body['grant_id'] and
                                 body['object_cookie'] in replacement['cookies'] and
                                 body['creator_transaction_id'] in replacement['txids'] and replacement['observed'] < seq)
            host.require(grant is not None and (grant['phase'] in ('admitted', 'settling', 'revoked') or
                         grant['phase'] == 'active' and (replacement_match or extra_match)) and obj is not None and creator is not None and
                         obj['owner_transaction'] == creator['id'] and creator['grant_id'] == body['grant_id'] and
                         creator['state'] == 'retired' and creator['result'] >= 0 and
                         (creator['returned_fd'] is not None or replacement_match and creator['invocation_id'] is None) and
                         creator['command'] in ('MAP_CREATE', 'PROG_LOAD') and
                         creator['output'] is not None and creator['output'].get('object_cookie') == body['object_cookie'],
                         'BPF grant cleanup lacks its retired successful original creator')
            if body['step'] == 1:
                host.require(prior is None and body['result'] == 0 and obj['removal_seq'] is None,
                             'BPF original retained reference was not closed exactly once')
                if grant['phase'] == 'admitted':
                    related = [txn for txn in transactions.values() if txn['grant_id'] == body['grant_id'] and
                               txn['invocation_id'] == creator['invocation_id'] and txn['state'] == 'retired' and
                               txn['id'] in audited_transactions]
                    def answered(txn, result):
                        reply = notification_responses.get(txn['id'])
                        return (reply is not None and reply['operation'] == 'SEND' and reply['ioctl_errno'] == 0 and
                                reply['response']['error'] == result)
                    feature = (creator['command'] == 'MAP_CREATE' and obj['attributes']['name'] == 'feature_test' and
                               any(txn['command'] == 'OBJ_GET_INFO_BY_FD' and txn['result'] == 0 and answered(txn, 0) and
                                   len(txn['input_objects']) == 1 and txn['input_objects'][0]['cookie'] == obj['cookie']
                                   for txn in related))
                    replace = (creator['command'] == 'PROG_LOAD' and creator['program_bytes'] == 'b7000000000000009500000000000000' and
                               any(txn['command'] == 'PROG_ATTACH' and txn['result'] == -9 and answered(txn, -9) and
                                   txn['input_objects'] and txn['input_objects'][0]['kind'] == 'dev_null' and
                                   any(item['kind'] == 'program' and item['cookie'] == obj['cookie'] for item in txn['input_objects'])
                                   for txn in related))
                    host.require(feature or replace or replacement_match or extra_match, 'BPF grant closed a non-probe creator before settlement')
            else:
                removal = removed.get(body['object_cookie'])
                host.require(prior is not None and prior['step'] == 1 and
                             all(prior[key] == body[key] for key in ('grant_id', 'creator_transaction_id', 'object_cookie')) and
                             removal is not None and obj['removal_seq'] == body['guard_sequence'] == removal['sequence'] and
                             body['result'] == removal['result'], 'BPF cleanup lacks the actual matching ID removal')
            grant_cleanup[body['object_cookie']] = body
        elif kind == 'registry':
            if body['map'] not in ('broker_token', 'guard_liveness'):
                group = enrollments.get(body['grant_id'])
                host.require(group is not None and body['key']['root_kn_id'] == group['record']['root_cookie'] and
                             body['key']['kn_id'] == group['record']['cgroup_id'] and
                             (body['value'] is None or body['value']['enrollment'] == group['record']['enrollment']),
                             'BPF registry record differs from retained cgroup enrollment')
            transaction = transactions.get(body['transaction_id'])
            host.require(transaction is not None and transaction['state'] == 'committed' and
                         transaction['grant_id'] is None and transaction['command'] in
                         ('MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM'), 'BPF registry action has no committed broker transaction')
            parent_id = body['parent_transaction_id']
            if parent_id is not None:
                parent = transactions.get(parent_id)
                grant = grants.get(body['grant_id'])
                host.require(parent is not None and parent['state'] == 'committed' and
                             parent['grant_id'] == body['grant_id'] and body['map'] in ('realm_targets', 'retire_tasks') and
                             (parent['command'] == 'PROG_ATTACH' or parent['command'] == 'PROG_DETACH' and
                              grant is not None and grant['retirement'] == 'explicit_cleanup') and
                             any(item['kind'] == 'cgroup' and item['cookie'] == group['record']['enrollment']
                                 for item in parent['input_objects']), 'BPF nested registry effect lacks its committed parent')
                if body['map'] == 'realm_targets':
                    host.require(body['value'] is not None and
                                 body['value']['retirement'] == (1 if grant['retirement'] == 'auto_empty' else 2) and
                                 ((body['action'] == 'insert' and body['value']['phase'] == 1) or
                                  (body['action'] == 'update' and body['value']['phase'] == 2)),
                                 'BPF nested target has an unauthorized state or retirement')
                elif body['value'] is not None:
                    actor_id = body['value']['invocation_id']
                    if actor_id == 0:
                        host.require(body['key']['tgid'] == 1, 'BPF nested systemd actor identity differs')
                    else:
                        invocation = invocations.get(parent['invocation_id'])
                        principal = None if invocation is None else principals.get(invocation['principal_id'])
                        observed_exec = [item for item in guard_records.values() if item['kind'] == 10 and
                                         principal is not None and item['task_cookie'] == principal['task_cookie'] and
                                         item['task_start_ns'] == principal['start_ns']]
                        host.require(actor_id == parent['invocation_id'] and principal is not None and observed_exec and
                                     body['key']['tgid'] == principal['tgid'] and
                                     body['key']['leader_start_ns'] == principal['start_ns'] and
                                     body['value']['exe_dev'] == observed_exec[-1]['detail']['exe_dev'] and
                                     body['value']['exe_ino'] == observed_exec[-1]['detail']['exe_ino'],
                                     'BPF nested runtime actor differs from its observed invocation')
                nested_registry.setdefault(parent_id, []).append(seq)
            key = (body['map'], _canonical(body['key']))
            previous = registries.get(key)
            grant = grants.get(body['grant_id'])
            if grant is not None and grant['phase'] == 'revoked':
                host.require(body['parent_transaction_id'] is None and previous is not None and
                             previous['grant_id'] == body['grant_id'] and
                             ((body['map'] == 'retire_tasks' and body['action'] == 'delete') or
                              (body['map'] == 'realm_targets' and body['action'] == 'update' and
                               body['value']['phase'] in (3, 4))),
                             'BPF revoked grant acquired a new registry permission')
            if body['action'] == 'insert':
                host.require(previous is None, 'BPF registry key was inserted twice')
                registries[key] = body['value']
                if (body['map'] == 'retire_tasks' and parent_id is None and
                        body['value']['invocation_id'] != 0):
                    host.require(key not in active_delete_permits, 'BPF delete permit was reused')
                    active_delete_permits[key] = {'sequence': seq, 'value': body['value'], 'grant_id': body['grant_id']}
            elif body['action'] == 'update':
                if body['map'] == 'guard_liveness':
                    host.require(previous is None, 'BPF guard liveness updated twice in bootstrap')
                else:
                    host.require(previous is not None and body['map'] == 'realm_targets' and
                                 all(previous[field] == body['value'][field] for field in ('enrollment', 'grant_id', 'retirement')) and
                                 body['value']['phase'] >= previous['phase'], 'BPF registry update changed an incarnation')
                registries[key] = body['value']
            else:
                host.require(previous is not None and body['map'] == 'retire_tasks', 'BPF registry deletion is not authorized')
                host.require(previous['grant_id'] == body['grant_id'], 'BPF registry deletion changed grant')
                deleted_registry_values[seq] = previous
                permit = active_delete_permits.pop(key, None)
                if permit is not None:
                    host.require(parent_id is None and previous == permit['value'] and permit['grant_id'] == body['grant_id'],
                                 'BPF delete permit retirement changed its exact identity')
                    retired_delete_permits[seq] = permit
                del registries[key]
            if body['map'] not in ('broker_token', 'guard_liveness'):
                host.require(body['grant_id'] in grants, 'BPF registry references an unknown grant')
            registry_records[seq] = body
        elif kind == 'audit' and body['record_type'] == 'SYSCALL' and body['transaction_id'] is not None:
            host.require(body['transaction_id'] not in syscall_audit_positions, 'BPF transaction syscall audit duplicated')
            syscall_audit_positions[body['transaction_id']] = seq
            audited_transactions.add(body['transaction_id'])
        elif kind == 'audit' and body['record_type'] == 'BPF':
            if body['operation'] == 'LOAD':
                audited_loads.add(body['object_cookie'])
            else:
                audited_unloads.add(body['object_cookie'])
        elif kind == 'notification_response':
            txn = transactions.get(body['transaction_id'])
            host.require(txn is not None and txn['state'] == 'effect_observed' and
                         body['transaction_id'] not in notification_responses and
                         all(txn[key] == body[key] for key in ('invocation_id', 'requester_cookie', 'notification_id')) and
                         body['guard_after'] <= guard_watermark, 'BPF response lacks its actual observed effect')
            needs_fd = txn['command'] in ('MAP_CREATE', 'PROG_LOAD', 'PROG_GET_FD_BY_ID') and txn['result'] >= 0
            host.require((not needs_fd and body['operation'] == 'SEND') or
                         (needs_fd and (body['operation'] == 'ADDFD_SEND' or
                                        body['operation'] == 'SEND' and body['response']['error'] == -1)),
                         'BPF response operation differs from its actual effect')
            notification_responses[body['transaction_id']] = body
        elif kind == 'notification_cancel':
            notification = (body['invocation_id'], body['notification_id'])
            matches = [txn for txn in transactions.values() if
                       (txn['invocation_id'], txn['notification_id']) == notification]
            host.require(len(matches) == 1 and matches[0]['state'] == 'prepared' and notification not in cancellations and
                         matches[0]['requester_cookie'] == body['requester_cookie'] and
                         _COMMAND_IDS.get(matches[0]['command']) == body['command'] and body['guard_after'] <= guard_watermark,
                         'BPF cancellation lacks its original prepared notification')
            cancellations[notification] = dict(body, transaction_id=matches[0]['id'], cancellation_sequence=seq)
        elif kind == 'notification_denial':
            invocation = invocations.get(body['invocation_id'])
            notification = (body['invocation_id'], body['notification_id'])
            host.require(invocation is not None and invocation['listener_id'] is not None and
                         notification not in notifications and body['guard_after'] <= guard_watermark,
                         'BPF denial lacks a unique observed listener invocation')
            notifications.add(notification)
            denials.append(body)
        elif kind == 'selftest_fd_stage':
            host.require(scenario == 'bpf-authorization', 'BPF FD selftest outside authorized case')
            if body['phase'] == 'armed':
                grant = grants.get(body['grant_id'])
                owner = None if grant is None else principals.get(grant['owner'])
                expected_class = ('map', 'program')[len(fd_stages)] if len(fd_stages) < 2 else None
                host.require(fd_active is None and body['class'] == expected_class and
                             grant is not None and grant['phase'] == 'admitted' and owner is not None and
                             owner['role'] == 'REALM_ROOT' and owner['registration'] == 'realm_self' and
                             owner['terminal'] is None and
                             all(item['nonce'] != body['nonce'] and item['grant_id'] != body['grant_id'] and
                                 grants[item['grant_id']]['owner'] == grant['owner'] for item in fd_stages),
                             'BPF FD selftest arm is not bound to an original root and distinct grant')
                fd_active = dict(body)
            elif body['phase'] == 'entered':
                txn = transactions.get(body['transaction_id'])
                host.require(fd_active is not None and fd_active['phase'] == 'armed' and
                             all(body[k] == fd_active[k] for k in ('nonce', 'grant_id', 'class')) and
                             txn is not None and txn['state'] == 'committed' and txn['grant_id'] == body['grant_id'] and
                             txn['command'] == ('MAP_CREATE' if body['class'] == 'map' else 'PROG_LOAD') and
                             (body['class'] != 'program' or txn['program_bytes'] == _device_program().hex()),
                             'BPF FD selftest entered without its committed creation')
                fd_active = dict(body)
            else:
                host.require(fd_active is not None and fd_active['phase'] == 'entered' and
                             all(body[k] == fd_active[k] for k in ('nonce', 'grant_id', 'class', 'transaction_id',
                                                                    'saved_soft', 'hard_limit', 'test_soft', 'original_fd_count',
                                                                    'identity_fd_count', 'owned_fd_count')),
                             'BPF FD selftest restoration does not match entry')
                fd_stages.append(dict(body))
                fd_active = dict(body)
        elif kind == 'checkpoint':
            if body['state'] == 'quiesced':
                owner = principals.get(body['principal_id'])
                host.require(checkpoint is None and body['nonce'] not in nonces and owner is not None and
                             owner['terminal'] is None and owner['role'] in ('BOOT_OWNER', 'REALM_ROOT', 'RECOVERY_OBSERVER') and
                             body['tail_before'] == hashes[seq - 1] and body['begin_watermark'] == guard_watermark and
                             body['begin_watermark'] == body['end_watermark'], 'BPF quiesce record differs')
                host.require(all(settled_or_retained(txn) for txn in transactions.values()),
                             'BPF checkpoint includes an unresolved effect or unaudited alias')
                nonces.add(body['nonce'])
                checkpoint = body
            elif body['state'] == 'failed':
                owner = principals.get(body['principal_id'])
                host.require(not checkpoint_failed and owner is not None and owner['terminal'] == 'exit' and
                             owner['role'] in ('BOOT_OWNER', 'REALM_ROOT', 'RECOVERY_OBSERVER') and
                             body['tail_before'] in hashes[:seq] and body['end_watermark'] == guard_watermark and
                             body['begin_watermark'] <= body['end_watermark'] and
                             ((checkpoint is None and body['nonce'] not in nonces) or
                              (checkpoint is not None and all(body[field] == checkpoint[field] for field in
                               ('nonce', 'begin_watermark', 'tail_before', 'principal_id')))),
                             'BPF failed checkpoint lacks its actual exited owner and observed cursor')
                nonces.add(body['nonce'])
                checkpoint = None
                checkpoint_failed = True
            else:
                host.require(checkpoint is not None and body['state'] == 'released' and
                             body['end_watermark'] == guard_watermark and
                             all(body[field] == checkpoint[field] for field in ('nonce', 'begin_watermark', 'tail_before', 'principal_id')),
                             'BPF checkpoint release has no matching quiesce')
                checkpoint = None
    host.require(not active_delete_permits, 'BPF bound delete permit remains active')
    for delete_seq, permit in retired_delete_permits.items():
        inserted = [txn for txn in transactions.values() if txn['command'] == 'MAP_LOOKUP_ELEM' and
                    txn['state'] == 'retired' and txn['result'] == 0 and txn['output'] is not None and
                    txn['output'].get('registry_sequence') == permit['sequence'] and txn['id'] in audited_transactions]
        absent = [txn for txn in transactions.values() if txn['command'] == 'MAP_LOOKUP_ELEM' and
                  txn['state'] == 'retired' and txn['result'] == -2 and
                  txn['output'] == {'registry_sequence': delete_seq, 'value_hex': None} and
                  txn['id'] in audited_transactions]
        deletion = transactions.get(registry_records[delete_seq]['transaction_id'])
        host.require(inserted and len(absent) == 1 and deletion is not None and deletion['state'] == 'retired' and
                     deletion['result'] == 0 and deletion['id'] in audited_transactions,
                     'BPF delete permit lacks audited insertion and exact absence readbacks')
    host.require(all(item['record']['source'] == 'bootstrap' or item['used'] for item in held_files.values()),
                 'BPF imported device has no matching prepared notification')
    host.require(all(group['used'] for group in enrollments.values()),
                 'BPF cgroup enrollment has no matching prepared notification')
    for attachment in attachments.values():
        transaction = transactions[attachment['transaction_id']]
        host.require(transaction['state'] == 'retired' and transaction['result'] == 0 and
                     transaction['output'] == {'attachment_id': attachment['id']},
                     'BPF attachment lacks its actual successful attach result')
        matches = [ident for ident, attachment_id in internal_queries.items() if attachment_id == attachment['id']]
        host.require(len(matches) == 1 and transactions[matches[0]]['state'] == 'retired' and
                     matches[0] in audited_transactions,
                     'BPF attachment lacks its independently audited post-attach query')
    broker = principals.get(prefix['broker_principal'])
    host.require(broker is not None and broker['role'] == 'BOOTSTRAP_BROKER' and broker['registration'] == 'bootstrap' and
                 broker['terminal'] is None and broker['exe_sha256'] == prefix['broker_sha256'] and
                 broker['uid'] == broker['gid'] == 0 and broker['tgid'] == broker['tid'],
                 'BPF bootstrap principal differs')
    token_values = [value for (name, _), value in registries.items() if name == 'broker_token']
    host.require(token_values == [{'cookie': broker['task_cookie']}], 'BPF broker token does not match the bootstrap task')
    host.require(link_facts == {link['hook']: link for link in prefix['links']}, 'BPF bootstrap links lack complete transactions')
    host.require(bootstrap_map_references <= set(objects) and all(objects[cookie]['class'] == 'map' for cookie in bootstrap_map_references),
                 'BPF bootstrap relocation map never resolved to an object')
    host.require(checkpoint is None, 'BPF final checkpoint was not released')
    host.require(all(settled_or_retained(txn) for txn in transactions.values()),
                 'BPF transaction remains pending without a known retained alias')
    for ident, grant_id in fd_registry_commits:
        txn = transactions[ident]
        output = txn['output']
        if txn['command'] == 'OBJ_GET_INFO_BY_FD':
            host.require(len(txn['input_objects']) == 1 and txn['input_objects'][0]['cookie'] in objects and
                         transactions[objects[txn['input_objects'][0]['cookie']]['owner_transaction']]['grant_id'] == grant_id,
                         'BPF selftest info transaction belongs to another grant')
        else:
            host.require(type(output) is dict and output.get('registry_sequence') in registry_records and
                         registry_records[output['registry_sequence']]['grant_id'] == grant_id,
                         'BPF selftest registry effect is not bound to the armed grant')
    host.require(set(publications) == set(objects), 'BPF publication has no recorded object incarnation')
    for cookie, obj in objects.items():
        txn = transactions.get(obj['owner_transaction'])
        host.require(txn is not None and txn['state'] == 'retired' and
                     txn['command'] == ('PROG_LOAD' if obj['class'] == 'program' else 'MAP_CREATE') and
                     (obj['class'] != 'program' or obj['bytes'] == txn['program_bytes']),
                     'BPF object has no matching settled creation transaction')
        if txn['grant_id'] is not None:
            attr = bytes.fromhex(txn['attributes']['attr_hex'])
            host.require(obj['type'] == struct.unpack_from('<I', attr)[0],
                         'BPF runtime object type differs from its allowed request')
            if obj['class'] == 'map':
                host.require(obj['attributes'] == {'name': 'feature_test', 'key_size': 4, 'value_size': 4,
                                                  'max_entries': 1, 'map_flags': 0, 'btf_id': 0,
                                                  'btf_key_type_id': 0, 'btf_value_type_id': 0, 'map_extra': 0},
                             'BPF feature map snapshot differs from the allowed map')
            else:
                host.require(obj['attributes']['name'] == '' and obj['attributes']['attach_btf_id'] == 0 and
                             obj['attributes']['created_by_uid'] == 0,
                             'BPF runtime program snapshot differs from broker creation context')
        infos = [item for item in transactions.values() if item['command'] == 'OBJ_GET_INFO_BY_FD' and
                 len(item['input_objects']) == 1 and item['input_objects'][0]['cookie'] == cookie and
                 item['input_objects'][0]['kind'] == obj['class'] and item['result'] is not None and item['result'] >= 0]
        if txn['result'] >= 0:
            host.require(infos and all(item['state'] == 'retired' and item['output'] is not None and
                                       item['output'].get('object_cookie') == cookie and
                                       item['output'].get('type') == obj['type'] and item['output'].get('kernel_id') == obj['kernel_id'] and
                                       item['output'].get('attributes') == obj['attributes'] and
                                       item['grant_id'] in (None, txn['grant_id']) and
                                       (item['grant_id'] is not None or item['input_objects'][0]['requester_fd'] == txn['result'])
                                       for item in infos),
                         'BPF held object lacks matching real FD information')
        else:
            host.require(txn['result'] < 0 and not infos and obj['removal_seq'] is not None,
                         'BPF failed creation has unproven publication or information')
        if cookie in removed:
            host.require(obj['removal_seq'] == removed[cookie]['sequence'],
                         'BPF observed removal was not applied to its incarnation')
    for txn in transactions.values():
        if txn['grant_id'] is not None and txn['result'] is not None and txn['command'] == 'PROG_LOAD' and txn['license'] == 'MIT':
            attr = bytes.fromhex(txn['attributes']['attr_hex'])
            if len(attr) >= 4 and struct.unpack_from('<I', attr)[0] == 1:
                host.require(txn['result'] == -9 and
                             not any(obj['owner_transaction'] == txn['id'] for obj in objects.values()),
                             'BPF extended-info prepublication failure produced an object')
    for cancellation in cancellations.values():
        txn = transactions[cancellation['transaction_id']]
        host.require(txn['state'] == 'retired' and txn['result'] is None and
                     not any(obj['owner_transaction'] == txn['id'] for obj in objects.values()) and
                     not any(item['transaction_id'] == txn['id'] for item in attachments.values()) and
                     not any(item['transaction_id'] == txn['id'] for item in registry_records.values()) and
                     all(sequence in guard_records for sequence in range(cancellation['guard_before'] + 1,
                                                                          cancellation['guard_after'] + 1)),
                     'BPF cancelled notification produced an effect or lacks complete observation')
        host.require(not any(prepared_sequences[txn['id']] < seq < cancellation['cancellation_sequence'] and
                             record['grant_id'] == txn['grant_id'] for seq, record in registry_records.items()),
                     'BPF precommit cancellation acquired a registry effect')
        host.require(not any(guard_records[sequence]['kind'] in (1, 3) and
                             guard_records[sequence]['task_cookie'] == txn['executor_cookie']
                             for sequence in range(cancellation['guard_before'] + 1, cancellation['guard_after'] + 1)),
                     'BPF cancelled notification overlaps a broker publication')
    for denial in denials:
        host.require(denial['guard_after'] <= guard_watermark and
                     all(sequence in guard_records for sequence in range(denial['guard_before'] + 1, denial['guard_after'] + 1)),
                     'BPF denial guard interval is incomplete')
    host.require(fd_active is None, 'BPF FD selftest remains active')
    if scenario == 'bpf-authorization':
        host.require(len(fd_stages) == 2, 'BPF FD selftest did not complete both classes')
    for stage in fd_stages:
        txn = transactions.get(stage['transaction_id'])
        produced = [item for item in objects.values() if item['owner_transaction'] == stage['transaction_id']]
        host.require(txn is not None and txn['state'] == 'retired' and txn['result'] == -24 and
                     txn['returned_fd'] is None and len(produced) == 1 and produced[0]['class'] == stage['class'] and
                     produced[0]['removal_seq'] is not None and produced[0]['cookie'] in removed and
                     not any(a['grant_id'] == stage['grant_id'] and a['state'] != 'released' for a in attachments.values()),
                     'BPF FD selftest did not observe and retire its real EMFILE incarnation')
        cleanup = txn['cleanup']
        host.require(cleanup['policy'] == 'close_unattached' and cleanup['state'] == 'settled' and
                     any(step['action'] == 'await_id_removed' and step['object_cookie'] == produced[0]['cookie'] and
                         step['guard_sequence'] == produced[0]['removal_seq'] for step in cleanup['steps']),
                     'BPF FD selftest did not settle its preauthorized cleanup')
        host.require(not any(t['command'] == 'OBJ_GET_INFO_BY_FD' and
                             any(i['cookie'] == produced[0]['cookie'] for i in t['input_objects'])
                             for t in transactions.values()), 'BPF FD-less object has fabricated info transaction')


def _replay_module_instance(host, records, prefix):
    instances = [event['body'] for event in records[:len(prefix['records'])] if event['kind'] == 'module_instance']
    brokers = [event['body'] for event in records[:len(prefix['records'])] if event['kind'] == 'principal' and
               event['body']['id'] == prefix['broker_principal']]
    host.require(len(instances) == 1 and len(brokers) == 1 and
                 brokers[0]['role'] == 'BOOTSTRAP_BROKER' and
                 instances[0]['loader_cookie'] == brokers[0]['parent_cookie'] and
                 instances[0]['module_sha256'] == prefix['guard_sha256'] and
                 instances[0]['observer_bindings_sha256'] == prefix['observer_bindings_sha256'],
                 'BPF native module instance is not bound to the selected loader and build')
    host.require(sum(event['kind'] == 'module_instance' for event in records) == 1,
                 'BPF module instance was replaced within a boot')
    return instances[0]


def _endpoint(host, endpoint):
    if endpoint is None:
        return
    host.require(_shape(endpoint, ('pid', 'start_ticks', 'netns_dev', 'netns_ino', 'network_id',
                                  'endpoint_id', 'ipv4', 'prefix', 'gateway', 'ipam_subnet', 'recovery_id')) and
                 all(_integer(endpoint[k], 1) for k in ('pid', 'start_ticks', 'netns_dev', 'netns_ino')) and
                 _hex(endpoint['network_id']) and _hex(endpoint['endpoint_id']) and
                 type(endpoint['prefix']) is int and endpoint['prefix'] == 24 and
                 all(type(endpoint[k]) is str and endpoint[k] for k in ('ipv4', 'gateway', 'ipam_subnet')) and
                 (endpoint['recovery_id'] is None or type(endpoint['recovery_id']) is str and
                  re.fullmatch(r'[a-z0-9_-]{1,128}', endpoint['recovery_id']) is not None),
                 'BPF endpoint projection invalid')
    import ipaddress
    net = ipaddress.IPv4Network(endpoint['ipam_subnet'], strict=True)
    address, gateway = ipaddress.IPv4Address(endpoint['ipv4']), ipaddress.IPv4Address(endpoint['gateway'])
    host.require(net.prefixlen == endpoint['prefix'] and address in net and gateway in net and
                 address != gateway and address not in (net.network_address, net.broadcast_address) and
                 gateway not in (net.network_address, net.broadcast_address), 'BPF endpoint IPAM binding invalid')


def _replay_exec_lifecycle(host, records, complete=True):
    principals, grants, admissions, invocations, transitions = {}, {}, {}, {}, {}
    active, docker_ids, task_forks, task_execs, task_exits = {}, {}, {}, {}, {}
    current_generation, current_endpoint, gate, gate_closed = 1, None, None, False
    for event in records:
        kind, body = event['kind'], event['body']
        if kind == 'guard':
            guard = body['record']
            cookie = guard['task_cookie']
            if guard['kind'] == 9:
                host.require(cookie not in task_forks, 'BPF exec task cookie was reused')
                task_forks[cookie] = guard
            elif guard['kind'] == 10:
                task_execs.setdefault(cookie, []).append(guard)
            elif guard['kind'] == 11:
                host.require(cookie not in task_exits, 'BPF exec task exit duplicated')
                task_exits[cookie] = guard
        if kind == 'principal':
            principals[body['id']] = body
        elif kind == 'grant':
            grants[body['id']] = body
        elif kind == 'invocation' and body['command'] == 'exec':
            previous = invocations.get(body['id'])
            if previous is None:
                adm = admissions.get(body['exec_id'])
                host.require(adm is not None and adm['state'] == 'admitted' and adm['docker_exec_id'] is not None and
                             adm['grant_id'] == body['grant_id'] and
                             all(inv['exec_id'] != body['exec_id'] for inv in invocations.values()),
                             'BPF exec invocation lacks its unique tracked admission')
            else:
                host.require(all(body[k] == previous[k] for k in body if k != 'state'),
                             'BPF exec invocation identity changed')
            invocations[body['id']] = body
        elif kind == 'exec_admission':
            _record_body(host, kind, body)
            _endpoint(host, body['endpoint'])
            grant, old = grants.get(body['grant_id']), admissions.get(body['exec_id'])
            owner = None if grant is None else principals.get(grant['owner'])
            host.require(grant is not None and owner is not None and
                         ((body['observation'] == 'pg_controldata' and owner['role'] == 'RECOVERY_OBSERVER' and
                           grant['purpose'].startswith('recovery-') and body['endpoint'] is None and body['endpoint_generation'] is None) or
                          (body['observation'] != 'pg_controldata' and owner['role'] == 'REALM_ROOT' and
                           grant['purpose'] == 'postgres-live-app' and _integer(body['endpoint_generation'], 1))),
                         'BPF exec admission owner or grant differs')
            if old is None:
                host.require(body['state'] == 'admitted' and grant['phase'] == 'active' and
                             body['docker_exec_id'] is None and body['task_cookie'] is None and body['process_sha256'] is None and
                             body['grant_id'] not in active and (body['observation'] == 'pg_controldata' or
                             gate is None and not gate_closed and body['endpoint'] is not None and
                             body['endpoint_generation'] == current_generation and
                             (current_endpoint is None or body['endpoint'] == current_endpoint)),
                             'BPF exec capture or admission overlaps or uses a stale endpoint')
                if body['endpoint'] is not None:
                    current_endpoint = body['endpoint']
                active[body['grant_id']] = body['exec_id']
            else:
                host.require(all(body[k] == old[k] for k in ('exec_id', 'grant_id', 'observation', 'endpoint_generation')) and
                             body['state'] in {'admitted': ('admitted', 'bound', 'cancelled'),
                                               'bound': ('bound', 'settled'), 'settled': (), 'cancelled': ()}[old['state']] and
                             (body['state'] == 'cancelled' or body['endpoint'] == old['endpoint']) and
                             (old['docker_exec_id'] is None or body['state'] == 'cancelled' or body['docker_exec_id'] == old['docker_exec_id']) and
                             (old['process_sha256'] is None or body['process_sha256'] == old['process_sha256']) and
                             (old['task_cookie'] is None or body['task_cookie'] == old['task_cookie']),
                             'BPF exec admission changed identity or moved backwards')
            if body['docker_exec_id'] is not None:
                host.require(_hex(body['docker_exec_id']) and docker_ids.get(body['docker_exec_id'], body['exec_id']) == body['exec_id'],
                             'BPF Docker exec ID is malformed or reused')
                docker_ids[body['docker_exec_id']] = body['exec_id']
            if body['state'] == 'admitted':
                host.require(body['task_cookie'] is None and body['process_sha256'] is None and
                             (old is None or old['docker_exec_id'] is None and body['docker_exec_id'] is not None),
                             'BPF admitted exec contains an invalid track or process')
            elif body['state'] in ('bound', 'settled'):
                matches = [v for v in invocations.values() if v['exec_id'] == body['exec_id']]
                host.require(len(matches) == 1 and body['docker_exec_id'] is not None and _hex(body['process_sha256']) and
                             matches[0]['process_sha256'] == body['process_sha256'] and
                             (body['state'] != 'settled' or _integer(body['task_cookie'], 1)),
                             'BPF bound exec process or task differs from its invocation')
                if body['state'] == 'settled':
                    cookie = body['task_cookie']
                    fork, exited = task_forks.get(cookie), task_exits.get(cookie)
                    executions = task_execs.get(cookie, [])
                    host.require(fork is not None and exited is not None and executions and
                                 fork['task_start_ns'] == exited['task_start_ns'] and
                                 fork['sequence'] < executions[-1]['sequence'] < exited['sequence'] and
                                 executions[-1]['task_start_ns'] == fork['task_start_ns'] and
                                 matches[0]['state'] in ('settled', 'failed'),
                                 'BPF settled exec lacks actual fork, exec, task exit and wrapper settlement')
                    visited = set()
                    while cookie != matches[0]['task_cookie']:
                        host.require(cookie not in visited and cookie in task_forks,
                                     'BPF exec task is not in the admitted wrapper branch')
                        visited.add(cookie)
                        cookie = task_forks[cookie]['parent_cookie']
            else:
                host.require(body['state'] == 'cancelled' and all(body[k] is None for k in
                             ('docker_exec_id', 'task_cookie', 'process_sha256', 'endpoint')),
                             'BPF cancelled exec has invented execution facts')
            if body['state'] in ('settled', 'cancelled'):
                active.pop(body['grant_id'], None)
            admissions[body['exec_id']] = body
        elif kind == 'endpoint_transition':
            _record_body(host, kind, body)
            _endpoint(host, body['endpoint'])
            old, grant = transitions.get(body['transition_id']), grants.get(body['grant_id'])
            owner = None if grant is None else principals.get(grant['owner'])
            host.require(grant is not None and grant['purpose'] == 'postgres-live-app' and owner is not None and
                         owner['role'] == 'REALM_ROOT' and grant['phase'] == 'active',
                         'BPF endpoint transition grant owner differs')
            if old is None:
                host.require(body['state'] == 'begun' and gate is None and not gate_closed and
                             body['grant_id'] not in active and body['endpoint'] is None and body['endpoint_generation'] is None,
                             'BPF endpoint transition began while an exec or another transition was active')
                gate = body['transition_id']
            else:
                host.require(old['state'] == 'begun' and gate == body['transition_id'] and
                             body['state'] in ('completed', 'closed') and
                             all(body[k] == old[k] for k in ('transition_id', 'grant_id', 'recovery_id', 'coordinator_cookie', 'scope_sha256')),
                             'BPF endpoint transition identity or lifecycle differs')
                if body['state'] == 'completed':
                    host.require(body['endpoint'] is not None and body['endpoint']['recovery_id'] == body['recovery_id'] and
                                 body['endpoint_generation'] == current_generation + 1,
                                 'BPF endpoint transition generation or recovery binding differs')
                    current_generation, current_endpoint = body['endpoint_generation'], body['endpoint']
                else:
                    host.require(body['endpoint'] is None and body['endpoint_generation'] is None,
                                 'BPF closed endpoint transition contains completion facts')
                    gate_closed = True
                gate = None
            transitions[body['transition_id']] = body
    if complete:
        host.require(not active and gate is None and
                     all(admissions[inv['exec_id']]['state'] == 'settled' for inv in invocations.values()),
                     'BPF exec or endpoint transition remains unsettled')


def _replay_transition_sources(host, records, result):
    transitions = [e['body'] for e in records if e['kind'] == 'endpoint_transition' and e['body']['state'] == 'completed']
    if not transitions:
        return
    scopes = result.get('migration_scopes', [])
    host.require(type(scopes) is list, 'BPF endpoint source coordinator scopes are malformed')
    guards = [e['body']['record'] for e in records if e['kind'] == 'guard' and
              e['body']['record']['kind'] in (9, 10, 11, 12)]
    for transition in transitions:
        task = [g for g in guards if g['task_cookie'] == transition['coordinator_cookie']]
        host.require(task and all(g['task_start_ns'] == task[0]['task_start_ns'] and
                                 g['detail']['tgid'] == task[0]['detail']['tgid'] for g in task),
                     'BPF endpoint coordinator lacks its native task incarnation')
        candidates = []
        for scope in scopes:
            if type(scope) is not dict or type(scope.get('enrollment')) is not dict or type(scope.get('retirement')) is not dict:
                continue
            enrollment, retirement = scope['enrollment'], scope['retirement']
            coordinator = enrollment.get('coordinator')
            if (type(coordinator) is dict and coordinator.get('pid') == task[0]['detail']['tgid'] and
                    coordinator.get('starttime') == task[0]['task_start_ns'] // 10000000 and
                    coordinator == retirement.get('coordinator') and retirement.get('coordinator_pidfd_exited') is True and
                    retirement.get('coordinator_direct_wait') is True):
                candidates.append(scope)
        host.require(len(candidates) <= 1 and transition['scope_sha256'] ==
                     hashlib.sha256(b'/sys/fs/cgroup/g2-c81-migration\n').hexdigest(),
                     'BPF endpoint transition source coordinator or scope differs')
        if candidates:
            acknowledgements = candidates[0]['retirement'].get('terminal_acks', [])
            host.require(type(acknowledgements) is list and all(type(ack) is dict and
                         ack.get('recovery_id') == transition['recovery_id'] for ack in acknowledgements),
                         'BPF endpoint transition recovery ID differs from source witness')


def _replay_raw_evidence(host, records, actor_records, *, fault=None, begin=0):
    # Observer transport can reorder audit and guard delivery. Join by the full
    # native stamp and exact wire occurrence, never by serial or PID alone.
    actors, probes = {}, {}
    for event in actor_records:
        if event['kind'] == 'principal':
            b = event['body']
            actors[b['task_cookie']] = b
        elif event['kind'] == 'probe_actor':
            b = event['body']
            _record_body(host, 'probe_actor', b)
            host.require(b['task_cookie'] not in probes and b['probe_cookie'] not in
                         {p['probe_cookie'] for p in probes.values()}, 'BPF probe identity reused')
            probes[b['task_cookie']] = b
    wires, counts, guards, audits, watches = {}, {}, {}, {}, {}
    for event in records:
        kind, body = event['kind'], event['body']
        if kind == 'audit_wire':
            _record_body(host, kind, body)
            stamp = _audit_stamp(host, body)
            base = (stamp, body['record_type'], body['payload_sha256'])
            host.require(body['occurrence'] == counts.get(base, 0) + 1, 'BPF raw wire occurrence is not continuous')
            counts[base] = body['occurrence']
            key = base + (body['occurrence'],)
            host.require(key not in wires, 'BPF raw audit wire occurrence reused')
            wires[key] = event['sequence']
        elif kind == 'guard':
            _record_body(host, kind, body)
            guard = body['record']
            if guard['kind'] == 12:
                watches.setdefault(guard['task_cookie'], []).append(guard)
            elif guard['kind'] == 13:
                detail = guard['detail']
                stamp = (detail['seconds'], detail['nanoseconds'] // 1000000, guard['audit_serial'])
                host.require(stamp not in guards, 'BPF raw native stamp reused')
                guards[stamp] = (event['sequence'], guard)
        elif kind == 'audit' and body['raw'] is not None:
            _record_body(host, kind, body)
            stamp = _audit_stamp(host, body)
            host.require(stamp not in audits, 'BPF raw typed audit stamp reused')
            audits[stamp] = (event['sequence'], body)
    used = set()
    for stamp, (sequence, audit) in audits.items():
        ref = audit['raw']
        native = guards.get(stamp)
        host.require(native is not None, 'BPF raw typed syscall lacks native guard fact')
        guard = native[1]
        detail = guard['detail']
        host.require(audit['task_cookie'] == guard['task_cookie'] and audit['result'] == guard['result'] and
                     audit['command'] in _COMMAND_IDS and _COMMAND_IDS[audit['command']] == guard['operation'] and
                     ref['tgid'] == detail['tgid'], 'BPF raw audit actor, command or result differs')
        for typ, hash_key, occurrence_key in ((1300, 'syscall_payload_sha256', 'syscall_occurrence'),
                                              (1320, 'eoe_payload_sha256', 'eoe_occurrence')):
            key = (stamp, typ, ref[hash_key], ref[occurrence_key])
            host.require(key in wires and key not in used, 'BPF raw syscall lacks its exact audit wire occurrence')
            used.add(key)
        host.require(sum(n for (s, typ, _), n in counts.items() if s == stamp and typ == 1300) == 1 and
                     sum(n for (s, typ, _), n in counts.items() if s == stamp and typ == 1320) == 1,
                     'BPF raw syscall audit occurrence is ambiguous')
    for stamp, (sequence, guard) in guards.items():
        detail = guard['detail']
        actor = probes.get(guard['task_cookie']) or actors.get(guard['task_cookie'])
        host.require(actor is not None and actor['tgid'] == detail['tgid'] and actor['tid'] == detail['tid'] and
                     actor['start_ns'] == guard['task_start_ns'], 'BPF raw native actor incarnation differs')
        if guard['task_cookie'] in probes:
            if actor['name'] == 'container-raw-denial':
                cases = [e['body'] for e in actor_records if e['kind'] == 'selftest_case' and
                         e['body']['name'] == 'container-bpf' and e['body']['actor_cookie'] == actor['task_cookie'] and
                         e['body']['phase'] == 'settled']
                invocations = [e['body'] for e in actor_records if e['kind'] == 'invocation' and cases and
                               e['body']['id'] == cases[0]['invocation_id']]
                grants = [e['body'] for e in actor_records if e['kind'] == 'grant' and invocations and
                          e['body']['id'] == invocations[0]['grant_id'] and e['body']['purpose'] == 'container-bpf-selftest']
                brokers = [e['body'] for e in actor_records if e['kind'] == 'principal' and
                           e['body']['role'] == 'BOOTSTRAP_BROKER']
                lineage = [e['body']['record'] for e in actor_records if e['kind'] == 'guard' and
                           e['body']['record']['task_cookie'] == actor['task_cookie'] and
                           e['body']['record']['kind'] in (9, 10)]
                host.require(len(cases) == 1 and invocations and grants and len(brokers) == 1 and
                             actor['listener_id'] is None and actor['exe_sha256'] == brokers[0]['exe_sha256'] and
                             any(g['kind'] == 10 and g['sequence'] < guard['sequence'] and
                                 g['task_start_ns'] == actor['start_ns'] and
                                 (g['detail']['exe_dev'], g['detail']['exe_ino']) == (detail['exe_dev'], detail['exe_ino'])
                                 for g in lineage), 'BPF container probe lacks its authenticated native lineage')
            else:
                parent = actors.get(actor['parent_cookie'])
                host.require(parent is not None and parent['role'] == 'REALM_ROOT' and actor['listener_id'] is None and
                             actor['name'] in ('raw-denial', 'post-death-raw', 'seal-detach-denied'),
                             'BPF raw probe parent or role differs')
                matched = [w for w in watches.get(guard['task_cookie'], ()) if
                           w['sequence'] < guard['sequence'] and w['task_start_ns'] == guard['task_start_ns'] and
                           w['parent_cookie'] == actor['parent_cookie'] and
                           all(w['detail'][k] == detail[k] for k in ('tgid', 'tid', 'exe_dev', 'exe_ino'))]
                host.require(len(matched) == 1, 'BPF raw probe lacks its actual watched incarnation')
        host.require(stamp in audits or fault and fault['audit'] and sequence > begin,
                     'BPF raw syscall lacks its typed audit and EOE join')
    return guards


def _replay_audit(host, records):
    transactions = {event['body']['id']: event['body'] for event in records if event['kind'] == 'transaction'}
    objects = {event['body']['cookie']: event['body'] for event in records if event['kind'] == 'object'}
    grant_closes = {event['body']['object_cookie']: event['body'] for event in records
                    if event['kind'] == 'grant_object_cleanup' and event['body']['step'] == 1}
    audited_objects = {}
    controls = {}
    for event in records:
        if event['kind'] == 'audit_control':
            body = event['body']
            stamp = _audit_stamp(host, body)
            host.require(stamp not in controls, 'BPF audit control stamp duplicated')
            controls[stamp] = body
    pending, ready, syscalls, wires, typed = {}, [], {}, {}, {}
    for event in records:
        kind, body = event['kind'], event['body']
        if kind == 'checkpoint':
            if body['state'] != 'failed':
                host.require(not pending and not ready, 'BPF checkpoint has unresolved audit events')
        elif kind == 'guard':
            guard = body['record']
            if guard['kind'] not in (7, 8):
                continue
            host.require(guard['task_cookie'] > 0 and guard['task_start_ns'] > 0 and
                         guard['object_cookie'] > 0 and guard['kernel_id'] > 0 and guard['operation'] in (0, 1),
                         'BPF audit guard event lacks incarnation')
            task = (guard['task_cookie'], guard['task_start_ns'])
            if guard['kind'] == 7:
                stack = pending.setdefault(task, [])
                host.require(len(stack) < 8, 'BPF audit nesting exceeds limit')
                stack.append(guard)
            else:
                stack = pending.get(task)
                host.require(bool(stack), 'BPF audit leave has no enter')
                entry = stack.pop()
                if not stack:
                    del pending[task]
                host.require(all(entry[field] == guard[field] for field in ('object_cookie', 'kernel_id', 'operation')) and
                             entry['flags'] & 6 == guard['flags'] & 6 and entry['sequence'] < guard['sequence'],
                             'BPF audit guard call did not settle')
                ready.append(guard)
        elif kind == 'audit_wire':
            stamp = _audit_stamp(host, body)
            key = (stamp, body['record_type'], body['payload_sha256'])
            host.require(body['occurrence'] == wires.get(key, 0) + 1, 'BPF audit wire occurrence differs')
            wires[key] = body['occurrence']
        elif kind == 'audit':
            stamp = _audit_stamp(host, body)
            record_type = 1334 if body['record_type'] == 'BPF' else 1300
            typed[(stamp, record_type)] = typed.get((stamp, record_type), 0) + 1
            if body['record_type'] == 'BPF':
                op = 0 if body['operation'] == 'LOAD' else 1
                matches = [index for index, item in enumerate(ready) if
                           item['object_cookie'] == body['object_cookie'] and item['operation'] == op and
                           bool(item['flags'] & 1) == (body['context'] == 'syscall') and
                           (item['detail']['seconds'], item['detail']['nanoseconds'] // 1000000,
                            item['audit_serial']) == stamp]
                host.require(bool(matches) and len({(ready[index]['task_cookie'], ready[index]['task_start_ns'])
                                                    for index in matches}) == 1,
                             'BPF audit event lacks an unambiguous guard incarnation')
                selected = ready.pop(matches[0])
                host.require(body['result'] is None and
                             (body['context'] == 'standalone' or body['task_cookie'] == selected['task_cookie']),
                             'BPF audit task or context differs')
                transaction = transactions.get(body['transaction_id'])
                host.require(transaction is not None and
                             (body['operation'] != 'LOAD' or transaction['command'] == 'PROG_LOAD'),
                             'BPF audit event lacks its authorized transaction')
                obj = objects.get(body['object_cookie'])
                host.require(obj is not None and obj['class'] == 'program' and obj['kernel_id'] == selected['kernel_id'] and
                             (body['operation'] != 'LOAD' or obj['owner_transaction'] == body['transaction_id']),
                             'BPF audit event identifies another program incarnation')
                if body['operation'] == 'UNLOAD':
                    cleanup = transaction['cleanup']
                    grant_close = grant_closes.get(body['object_cookie'])
                    host.require((cleanup['policy'] in ('close_unattached', 'detach_then_close', 'auto_empty') and
                                  any(step['object_cookie'] == body['object_cookie'] for step in cleanup['steps'])) or
                                 (grant_close is not None and grant_close['creator_transaction_id'] == obj['owner_transaction'] and
                                  grant_close['grant_id'] == transactions[obj['owner_transaction']]['grant_id']),
                                 'BPF unload lacks its preauthorized object cleanup')
                key = (body['object_cookie'], body['operation'])
                host.require(key not in audited_objects, 'BPF object audit operation duplicated')
                audited_objects[key] = body
            else:
                host.require(stamp not in syscalls and body['context'] == 'syscall' and
                             body['object_cookie'] is None and body['result'] is not None,
                             'BPF syscall audit event duplicated or invalid')
                transaction = transactions.get(body['transaction_id'])
                if transaction is None and body['raw'] is None:
                    control = controls.get(stamp)
                    host.require(body['transaction_id'] is None and body['command'] is None and control is not None and
                                 control['phase'] == 'unicast_claim' and
                                 body['task_cookie'] == control['actor']['task_cookie'] and body['result'] >= 0,
                                 'BPF audit control syscall lacks its authenticated claim')
                elif transaction is None:
                    host.require(body['transaction_id'] is None, 'BPF raw audit transaction differs')
                else:
                    host.require(body['command'] == transaction['command'] and body['result'] == transaction['result'] and
                                 body['task_cookie'] == transaction['executor_cookie'],
                                 'BPF syscall audit result or executor differs')
                syscalls[stamp] = body
    host.require(not pending and not ready, 'BPF audit guard events remain unresolved')
    for cookie, obj in objects.items():
        if obj['class'] == 'program' and transactions[obj['owner_transaction']]['result'] >= 0:
            host.require((cookie, 'LOAD') in audited_objects, 'BPF successful program has no actual LOAD audit')
        if obj['class'] == 'program' and obj['removal_seq'] is not None and (cookie, 'LOAD') in audited_objects:
            host.require((cookie, 'UNLOAD') in audited_objects, 'BPF removed loaded program has no actual UNLOAD audit')
    counts = {}
    for (stamp, record_type, _), count in wires.items():
        counts[(stamp, record_type)] = counts.get((stamp, record_type), 0) + count
    for key, count in typed.items():
        host.require(counts.get(key, 0) == count, 'BPF typed audit and multicast multiplicity differ')
    for key, count in counts.items():
        if key[1] == 1334:
            host.require(typed.get(key, 0) == count, 'BPF multicast record lacks a typed occurrence')
    audited = [body['transaction_id'] for body in syscalls.values() if body['transaction_id'] is not None]
    host.require(len(audited) == len(set(audited)) and
                 set(audited) == {ident for ident, txn in transactions.items() if txn['result'] is not None},
                 'BPF actual syscall evidence does not cover every transaction')
    for event in records:
        if event['kind'] == 'audit':
            body = event['body']
            if body['context'] == 'syscall':
                stamp = _audit_stamp(host, body)
                syscall = syscalls.get(stamp)
                host.require(syscall is not None and syscall['task_cookie'] == body['task_cookie'] and
                             counts.get((stamp, 1320), 0) == 1, 'BPF audit syscall lacks matching syscall or EOE')


def _audit_rules():
    def rule(arch, calls):
        words = [0] * 260
        words[0:3] = [4, 2, 1]
        for call in calls:
            words[3 + call // 32] |= 1 << (call % 32)
        words[67], words[131], words[195] = 11, arch, 0x40000000
        return struct.pack('<260I', *words)
    return (rule(0xc000003e, (321,)), rule(0x40000003, (357,)),
            rule(0xc000003e, (49, 59, 322)), rule(0x40000003, (11, 102, 358, 361)))


def _audit_queries(host, body, previous_sequence, port):
    rules, status = [], None
    expected = (1000,) if body['check'] == 'status' else (1000, 1013, 1000)
    for query, request_type in zip(body['queries'], expected):
        host.require(_shape(query, ('request_type', 'request_sequence', 'request_portid', 'replies')) and
                     type(query['request_type']) is int and query['request_type'] == request_type and
                     _integer(query['request_sequence'], previous_sequence + 1, (1 << 32) - 1) and
                     _integer(query['request_portid'], 1, (1 << 32) - 1) and
                     (port is None or query['request_portid'] == port) and
                     type(query['replies']) is list and 1 <= len(query['replies']) <= 5,
                     'BPF audit query identity invalid')
        previous_sequence, port = query['request_sequence'], query['request_portid']
        done = False
        for index, reply in enumerate(query['replies']):
            host.require(_shape(reply, ('source_pid', 'source_groups', 'length', 'type', 'flags', 'sequence', 'header_pid', 'payload_hex')) and
                         all(_integer(reply[key], high=(1 << 32) - 1) for key in ('source_pid', 'source_groups', 'length', 'sequence', 'header_pid')) and
                         all(_integer(reply[key], high=65535) for key in ('type', 'flags')) and
                         reply['source_pid'] == reply['source_groups'] == reply['header_pid'] == 0 and
                         reply['sequence'] == previous_sequence and type(reply['payload_hex']) is str and
                         re.fullmatch(r'(?:[0-9a-f]{2})*', reply['payload_hex']) is not None and not done,
                         'BPF audit response framing invalid')
            payload = bytes.fromhex(reply['payload_hex'])
            host.require(reply['length'] == len(payload) + 16, 'BPF audit response length invalid')
            if request_type == 1000:
                host.require(len(query['replies']) == 1 and reply['type'] == 1000 and
                             reply['flags'] == 0 and len(payload) == 44, 'BPF audit GET response invalid')
                values = struct.unpack('<11I', payload)
                # Queue length and accumulated backlog wait can change while a query is in flight.
                fixed = values[:7] + values[8:10]
                host.require(status is None or fixed == status, 'BPF audit GET bracket changed')
                status = fixed
            elif reply['type'] == 3:
                host.require(index == len(query['replies']) - 1 and reply['flags'] == 2 and
                             payload in (b'', bytes(4)), 'BPF audit LIST terminal invalid')
                done = True
            else:
                host.require(reply['type'] == 1013 and reply['flags'] == 2 and len(payload) >= 1040 and
                             struct.unpack_from('<I', payload, 1036)[0] == len(payload) - 1040 and
                             payload not in rules and payload in _audit_rules(), 'BPF audit rule response invalid')
                rules.append(payload)
        if request_type == 1013:
            host.require(done and len(rules) in (2, 4), 'BPF audit LIST incomplete')
    return status, rules, previous_sequence, port


def _replay_readbacks(host, records, hashes, prefix):
    principals, reader = {}, None
    last_request, last_query, port, configs, cut, active = 0, 0, None, [], None, None
    wire_count, control_phase, control_claim = 0, 0, False
    for event in records:
        kind, body, sequence = event['kind'], event['body'], event['sequence']
        if kind == 'principal':
            principals[body['id']] = body
            if body['role'] == 'BOOTSTRAP_AUDIT_READER':
                host.require(body['registration'] == 'bootstrap' and body['uid'] == body['gid'] == 0 and
                             body['tgid'] == body['tid'] and body['exe_sha256'] == prefix['broker_sha256'] and
                             (reader is None or reader == body['task_cookie']), 'BPF audit reader principal differs')
                reader = body['task_cookie']
        elif kind == 'transaction' and body['state'] == 'committed':
            host.require(reader is not None and configs, 'BPF effect committed before authenticated initial audit readback')
        elif kind == 'grant':
            host.require(len(configs) >= 2, 'BPF ordinary grant preceded final locked audit readback')
        elif kind == 'checkpoint':
            if body['state'] == 'quiesced':
                active = body['principal_id']
            else:
                active = None
        elif kind == 'audit_wire':
            wire_count += 1
        elif kind == 'audit_readback':
            host.require(reader is not None and body['reader_cookie'] == reader and body['request_id'] == last_request + 1,
                         'BPF audit readback reader or sequence differs')
            last_request = body['request_id']
            status, rules, last_query, port = _audit_queries(host, body, last_query, port)
            host.require(status[2] == 1 and status[4] == 0 and status[5] == 16384 and status[6] == 0,
                         'BPF audit readback status differs')
            if body['check'] == 'configuration':
                if not configs:
                    host.require(status[1] == 1 and status[3] == 0 and set(rules) == set(_audit_rules()[:2]),
                                 'BPF initial audit rules differ')
                elif len(configs) == 1:
                    host.require(status[1] == 2 and status[3] == 0 and set(rules) == set(_audit_rules()),
                                 'BPF final audit rules differ')
                else:
                    owner = principals.get(active)
                    host.require(len(configs) == 2 and owner is not None and
                                 owner['role'] in ('REALM_ROOT', 'RECOVERY_OBSERVER') and status[1] == 2 and
                                 status[3] == owner['tgid'] and set(rules) == set(_audit_rules()),
                                 'BPF owner audit rules differ')
                configs.append((sequence, status))
            else:
                owner_pid = configs[-1][1][3] if configs else 0
                host.require(len(configs) >= 2 and status[1] == 2 and status[3] == owner_pid,
                             'BPF status differs from the last authenticated configuration')
        elif kind == 'audit_control':
            phases = ('early_setup', 'final_rules', 'final_lock', 'unicast_claim')
            phase = phases.index(body['phase'])
            host.require(phase >= control_phase and body['result'] == 1, 'BPF audit control phase or result invalid')
            control_phase = phase
            actor = body['actor']
            broker = principals.get(prefix['broker_principal'])
            if phase < 3:
                host.require(broker is not None and actor['task_cookie'] == broker['parent_cookie'] and
                             actor['tgid'] == actor['tid'] == 1 and actor['exe_sha256'] == prefix['broker_sha256'],
                             'BPF setup control actor is not pinned PID1')
                host.require((phase == 0 and body['change'] in ('enabled', 'failure', 'rate_limit', 'backlog_limit', 'add_rule')) or
                             (phase == 1 and body['change'] == 'add_rule') or
                             (phase == 2 and body['change'] == 'enabled' and body['old_value'] == 1 and body['new_value'] == 2),
                             'BPF setup control change exceeds its phase')
            else:
                principal = principals.get(active)
                host.require(not control_claim and principal is not None and
                             principal['role'] in ('REALM_ROOT', 'RECOVERY_OBSERVER') and
                             all(actor[key] == principal[key] for key in ('task_cookie', 'tgid', 'tid', 'start_ns', 'exe_sha256')) and
                             body['change'] == 'audit_pid' and body['old_value'] == 0 and body['new_value'] == principal['tgid'],
                             'BPF audit owner claim differs')
                control_claim = True
        elif kind == 'audit_cut':
            principal = principals.get(body['principal_id'])
            host.require(cut is None and active == body['principal_id'] and principal is not None and
                         principal['role'] in ('REALM_ROOT', 'RECOVERY_OBSERVER') and len(configs) >= 3 and
                         body['readback_sequence'] == configs[-1][0] and configs[-1][1][3] == principal['tgid'] and
                         body['readback_sequence'] <= body['boundary_sequence'] < sequence and
                         hashes[body['boundary_sequence']] == body['boundary_sha256'] and
                         body['wire_count'] == sum(1 for item in records[:body['boundary_sequence']] if item['kind'] == 'audit_wire') and
                         body['wire_count'] <= wire_count, 'BPF audit cut does not bind its readback and prefix')
            prefix_events = {}
            for item in records[:body['boundary_sequence']]:
                if item['kind'] == 'audit_wire':
                    wire = item['body']
                    key = (_audit_stamp(host, wire), wire['record_type'])
                    prefix_events[key] = prefix_events.get(key, 0) + 1
            for (stamp, record_type), count in prefix_events.items():
                if record_type == 1300:
                    host.require(count == 1 and prefix_events.get((stamp, 1320)) == 1,
                                 'BPF audit cut contains an incomplete syscall event')
            cut = body
    host.require(reader is not None and len(configs) >= 2, 'BPF authenticated initial and final audit readbacks missing')
    if any(value['role'] in ('REALM_ROOT', 'RECOVERY_OBSERVER') for value in principals.values()):
        host.require(cut is not None and len(configs) == 3, 'BPF principal audit cut missing')
    else:
        host.require(len(configs) == 2 and cut is None, 'BPF standalone audit configuration differs')


def _bootstrap_policy(host, records, prefix, policy_elf, target_btf):
    import source_bpf_core
    objects = {}
    transactions = {}
    for event in records[:len(prefix['records'])]:
        body = event['body']
        if event['kind'] == 'object':
            objects[body['cookie']] = body
        elif event['kind'] == 'transaction' and body['state'] == 'retired':
            transactions[body['id']] = body
    btf_loads = [value for value in transactions.values() if value['grant_id'] is None and value['command'] == 'BTF_LOAD']
    host.require(len(btf_loads) == 1 and _integer(btf_loads[0]['result'], high=(1 << 31) - 1) and
                 _shape(btf_loads[0]['output'], ('btf_bytes', 'btf_sha256')), 'BPF bootstrap BTF load missing')
    loaded_btf = source_bpf_core._BTF(bytes.fromhex(btf_loads[0]['output']['btf_bytes']))
    map_specs = {'broker_token': (29, 4, 8, 0, 1),
                 'guard_liveness': (2, 4, 8, 1, 0),
                 'realm_targets': (1, 16, 24, 128, 0),
                 'retire_tasks': (1, 32, 40, 4096, 0)}
    maps = {}
    map_fds = {}
    for item in objects.values():
        name = item['attributes']['name']
        if item['class'] != 'map' or name not in map_specs:
            continue
        attrs = item['attributes']
        host.require(name not in maps and item['state'] == 'live' and
                     (item['type'], attrs['key_size'], attrs['value_size'], attrs['max_entries'], attrs['map_flags']) == map_specs[name] and
                     attrs['map_extra'] == 0, 'BPF bootstrap map shape or identity differs')
        transaction = transactions.get(item['owner_transaction'])
        host.require(transaction is not None and transaction['grant_id'] is None and transaction['command'] == 'MAP_CREATE' and
                     _integer(transaction['result'], high=(1 << 31) - 1) and
                     transaction['output'] == {'object_cookie': item['cookie']}, 'BPF bootstrap map create is not bound')
        raw_attr = bytes.fromhex(transaction['attributes']['attr_hex'])
        host.require(len(raw_attr) == 72 and struct.unpack_from('<I', raw_attr, 48)[0] == btf_loads[0]['result'] and
                     struct.unpack_from('<II', raw_attr, 52) == (attrs['btf_key_type_id'], attrs['btf_value_type_id']) and
                     attrs['btf_id'] > 0 and loaded_btf.size(attrs['btf_key_type_id']) == attrs['key_size'] and
                     loaded_btf.size(attrs['btf_value_type_id']) == attrs['value_size'],
                     'BPF bootstrap map actual BTF binding differs')
        expected_attr = bytearray(72)
        struct.pack_into('<IIIII', expected_attr, 0, *map_specs[name])
        encoded_name = name.encode('ascii')
        expected_attr[28:28 + len(encoded_name)] = encoded_name
        struct.pack_into('<III', expected_attr, 48, btf_loads[0]['result'], attrs['btf_key_type_id'], attrs['btf_value_type_id'])
        host.require(raw_attr == bytes(expected_attr), 'BPF bootstrap map request differs')
        maps[name], map_fds[name] = item, transaction['result']
    host.require(set(maps) == set(map_specs) and len(set(map_fds.values())) == 4,
                 'BPF bootstrap map inventory incomplete')
    host.require(len({item['attributes']['btf_id'] for item in maps.values()}) == 1,
                 'BPF bootstrap maps do not share their actual BTF object')
    for mname in ('broker_token', 'guard_liveness'):
        for field, width, encoding in (('btf_key_type_id', 4, 0x01000020), ('btf_value_type_id', 8, 0x40)):
            _, scalar = loaded_btf.resolve(maps[mname]['attributes'][field])
            host.require(scalar['kind'] == 1 and scalar['value'] == width and
                         struct.unpack('<I', scalar['data'])[0] == encoding,
                         'BPF bootstrap scalar BTF differs')
    expected_types = {'realm_targets': (('target_key', ('root_kn_id', 'kn_id'), (0, 64)),
                                       ('target_value', ('enrollment', 'grant_id', 'phase', 'retirement'), (0, 64, 128, 160))),
                      'retire_tasks': (('retire_key', ('root_kn_id', 'kn_id', 'tgid', 'pad', 'leader_start_ns'), (0, 64, 128, 160, 192)),
                                       ('retire_value', ('enrollment', 'grant_id', 'invocation_id', 'exe_dev', 'exe_ino'), (0, 64, 128, 192, 256)))}
    for name, types in expected_types.items():
        for field, (type_name, member_names, member_offsets) in zip(('btf_key_type_id', 'btf_value_type_id'), types):
            type_id = maps[name]['attributes'][field]
            resolved_id, resolved = loaded_btf.resolve(type_id)
            members = loaded_btf.members(type_id)
            host.require(resolved['kind'] == 4 and resolved['name'] == type_name and
                         resolved_id == loaded_btf.unique(4, type_name) and
                         tuple(member[0] for member in members) == member_names and
                         tuple(member[2] for member in members) == member_offsets and
                         all(member[3] == 0 and loaded_btf.size(member[1]) ==
                             (4 if member[0] in ('phase', 'retirement', 'tgid', 'pad') else 8) for member in members),
                         'BPF bootstrap map BTF type layout differs')
    expected = source_bpf_core.expected_programs(policy_elf, target_btf, map_fds)
    host.require(type(expected) is dict and set(expected) == {'bpf', 'inode_rmdir', 'inode_mkdir'},
                 'BPF independent policy reconstruction incomplete')
    names = {'bpf': 'g2_bpf', 'inode_rmdir': 'g2_inode_rmdir', 'inode_mkdir': 'g2_inode_mkdir'}
    for link in prefix['links']:
        hook = link['hook']
        program = objects.get(link['program_cookie'])
        target = expected[hook]
        host.require(type(target) is dict and set(target) == {'bytes', 'attach_btf_id', 'map_relocations'} and
                     type(target['bytes']) is bytes and type(target['map_relocations']) is list and
                     _integer(target['attach_btf_id'], 1, (1 << 32) - 1) and
                     program is not None and program['class'] == 'program' and program['state'] == 'live' and
                     program['type'] == 29 and program['attributes']['name'] == names[hook] and
                     program['attributes']['attach_btf_id'] == target['attach_btf_id'] and
                     program['attributes']['created_by_uid'] == 0 and
                     program['bytes'] == target['bytes'].hex(), 'BPF bootstrap program differs from pinned ELF and kernel BTF')
        transaction = transactions.get(program['owner_transaction'])
        host.require(transaction is not None and transaction['grant_id'] is None and transaction['command'] == 'PROG_LOAD' and
                     transaction['result'] >= 0 and transaction['program_bytes'] == program['bytes'] and
                     transaction['license'] == 'GPL' and
                     _shape(transaction['output'], ('object_cookie', 'log_true_size', 'func_info_hex')) and
                     transaction['output']['object_cookie'] == program['cookie'] and
                     _integer(transaction['output']['log_true_size'], high=(1 << 32) - 1),
                     'BPF bootstrap program load is not bound')
        attr = bytes.fromhex(transaction['attributes']['attr_hex'])
        expected_attr = bytearray(144)
        struct.pack_into('<II', expected_attr, 0, 29, len(target['bytes']) // 8)
        encoded_name = names[hook].encode('ascii')
        expected_attr[48:48 + len(encoded_name)] = encoded_name
        struct.pack_into('<IIII', expected_attr, 68, 27, btf_loads[0]['result'], 8, 0)
        struct.pack_into('<I', expected_attr, 88, 1)
        struct.pack_into('<I', expected_attr, 108, target['attach_btf_id'])
        function_id = loaded_btf.unique(12, names[hook])
        host.require(attr == bytes(expected_attr) and transaction['output']['func_info_hex'] ==
                     struct.pack('<II', 0, function_id).hex(),
                     'BPF bootstrap program attributes or actual function-info differ')
        expected_inputs = [{'field': 'insns[%d].map_fd' % offset, 'kind': 'map',
                            'cookie': maps[name]['cookie'], 'requester_fd': map_fds[name]}
                           for offset, name in target['map_relocations']]
        host.require(transaction['input_objects'] == expected_inputs, 'BPF bootstrap ELF map relocations differ')


def _terminal_status(host, status):
    fields = ('state', 'sticky', 'authority_phase', 'sequence', 'ack_cursor', 'observer_ack_cursor',
              'lost', 'probe_missed', 'broker_cookie', 'observer_cookie', 'death_sequence', 'module_instance',
              'module_dev', 'module_ino', 'module_size', 'epoch', 'link_ids', 'program_ids', 'link_attached',
              'bootstrap_digest', 'observer_bindings_sha256')
    host.require(_shape(status, fields) and status['state'] in ('unsealed', 'sealed', 'dead') and
                 type(status['authority_phase']) is int and status['authority_phase'] in (0, 1, 2) and
                 all(_integer(status[k]) for k in ('sticky', 'sequence', 'ack_cursor', 'observer_ack_cursor',
                                                   'lost', 'probe_missed', 'module_dev')) and
                 all(_integer(status[k], 1) for k in ('broker_cookie', 'observer_cookie', 'module_instance', 'module_ino', 'module_size')) and
                 (status['death_sequence'] is None or _integer(status['death_sequence'], 1, status['sequence'])) and
                 status['ack_cursor'] <= status['sequence'] and status['observer_ack_cursor'] <= status['sequence'] and
                 (status['epoch'] is None or _hex(status['epoch'], _HEX32)) and
                 (status['bootstrap_digest'] is None or _hex(status['bootstrap_digest'])) and
                 _hex(status['observer_bindings_sha256']), 'BPF terminal native status schema invalid')
    for key in ('link_ids', 'program_ids', 'link_attached'):
        host.require(type(status[key]) is list and len(status[key]) == 3, 'BPF terminal link status invalid')
    host.require(all(v is None or _integer(v, 1, (1 << 32) - 1) for v in status['link_ids'] + status['program_ids']) and
                 all(type(v) is bool for v in status['link_attached']), 'BPF terminal link identities invalid')


def _terminal_exit(host, value, guards, kind, *, required=True):
    if value is None:
        host.require(not required, 'BPF terminal exit proof missing')
        return
    host.require(_shape(value, ('task_cookie', 'tgid', 'tid', 'start_ns', 'pidfd_exited',
                                'guard_exit_sequence', 'guard_exit_code', 'direct_wait')) and
                 all(_integer(value[k], 1) for k in ('task_cookie', 'tgid', 'tid', 'start_ns')) and
                 value['pidfd_exited'] is True and
                 (value['guard_exit_sequence'] is None or _integer(value['guard_exit_sequence'], 1)) and
                 _integer(value['guard_exit_code'], -(1 << 31), (1 << 31) - 1),
                 'BPF terminal exit identity invalid')
    wait = value['direct_wait']
    if wait is not None:
        host.require(_shape(wait, ('waiter_cookie', 'wait_status')) and _integer(wait['waiter_cookie'], 1) and
                     _integer(wait['wait_status'], high=65535) and wait['wait_status'] == value['guard_exit_code'],
                     'BPF terminal direct wait does not match the actual exit')
    if value['guard_exit_sequence'] is not None:
        guard = guards.get(value['guard_exit_sequence'])
        host.require(guard is not None and guard['kind'] == kind and guard['task_cookie'] == value['task_cookie'] and
                     guard['task_start_ns'] == value['start_ns'] and guard['detail']['tgid'] == value['tgid'] and
                     guard['detail']['tid'] == value['tid'] and guard['detail']['exit_code'] == value['guard_exit_code'],
                     'BPF terminal exit does not join its actual native task')
    else:
        host.require(not required, 'BPF terminal native exit missing')


def _verify_terminal_observation_obj(host, term_obs, expected_variant, expected_outcomes, *, prefix=None, actor_records=()):
    if type(expected_outcomes) is str:
        expected_outcomes = (expected_outcomes,)
    host.require(type(term_obs) is dict and
                 _shape(term_obs, ('version', 'variant', 'boot_id', 'epoch', 'module_instance',
                                   'observer_principal', 'authority_before', 'authority_after', 'terminal_sha256',
                                   'begin_sequence', 'end_sequence', 'broker_exit',
                                   'probe_exits', 'records', 'losses', 'outcome', 'fault_evidence')) and
                 type(term_obs['version']) is int and term_obs['version'] == 1 and term_obs['variant'] == expected_variant and
                 type(term_obs['boot_id']) is str and _UUID.fullmatch(term_obs['boot_id']) is not None and
                 (term_obs['epoch'] is None or _hex(term_obs['epoch'], _HEX32)) and
                 _integer(term_obs['module_instance'], 1) and _integer(term_obs['observer_principal'], 1) and
                 _integer(term_obs['begin_sequence']) and _integer(term_obs['end_sequence'], term_obs['begin_sequence']) and
                 _hex(term_obs['terminal_sha256'], nonzero=False) and type(term_obs['records']) is list and
                 type(term_obs['probe_exits']) is list and _shape(term_obs['losses'], ('native', 'audit', 'output')) and
                 all(type(term_obs['losses'][k]) is bool for k in ('native', 'audit', 'output')) and
                 term_obs['outcome'] in expected_outcomes, 'BPF terminal observation schema or outcome invalid')
    early = term_obs['outcome'] == 'early_no_handoff'
    losses = term_obs['losses']
    if term_obs['outcome'] in ('denial_proved', 'early_no_handoff'):
        host.require(term_obs['fault_evidence'] is None, 'BPF non-fault terminal has unsolicited fault evidence')
    if early:
        host.require(expected_variant in _EARLY_SEAL_VARIANTS and not any(losses.values()),
                     'BPF early observation has an invalid seal stage or observation loss')
    elif term_obs['outcome'] == 'denial_proved':
        host.require(not any(losses.values()), 'BPF denial proof has observation loss')
    else:
        host.require(expected_variant in _TERMINAL_FAULT_VARIANTS | _READER_FAULT_VARIANTS and not losses['output'],
                     'BPF undeclared terminal fault or output loss')
        if expected_variant in ('guard-overflow', 'guard-miss', 'guard-pairing'):
            host.require(losses['native'] and not losses['audit'], 'BPF native guard loss differs')
        else:
            host.require(not any(losses.values()), 'BPF independent observer suffered additional loss')
    before, after = term_obs['authority_before'], term_obs['authority_after']
    _terminal_status(host, before)
    _terminal_status(host, after)
    host.require(all(before[k] == after[k] for k in ('broker_cookie', 'observer_cookie', 'module_instance',
                 'module_dev', 'module_ino', 'module_size', 'observer_bindings_sha256')) and
                 after['module_instance'] == term_obs['module_instance'] and after['epoch'] == term_obs['epoch'] and
                 after['sequence'] >= before['sequence'] and
                 (early or before['epoch'] == term_obs['epoch']), 'BPF terminal status changed boot or module identity')
    if not early:
        host.require(before['state'] == 'sealed' and before['authority_phase'] == 1 and
                     before['sticky'] == before['lost'] == before['probe_missed'] == 0 and
                     all(before['link_attached']) and all(after['link_attached']) and
                     before['link_ids'] == after['link_ids'] and before['program_ids'] == after['program_ids'] and
                     before['bootstrap_digest'] == after['bootstrap_digest'], 'BPF terminal sealed link custody differs')
    if not losses['native']:
        host.require(after['sticky'] == after['lost'] == after['probe_missed'] == 0,
                     'BPF terminal status hides native loss')
    _json(host, term_obs)
    host.require(len(_canonical(term_obs)) <= _MAX_EVIDENCE_BYTES, 'BPF terminal observation exceeds result limit')
    records, previous, guard_sequence, guards, epoch_seen = term_obs['records'], _ZERO, 0, {}, False
    host.require(term_obs['end_sequence'] == len(records), 'BPF terminal observer final cursor differs')
    charged = 0
    for index, event in enumerate(records, 1):
        host.require(_shape(event, _ENVELOPE) and type(event['version']) is int and event['version'] == 1 and
                     event['boot_id'] == term_obs['boot_id'] and event['sequence'] == index and
                     event['previous'] == previous and event['kind'] in ('guard', 'audit_wire', 'audit', 'audit_readback', 'reader_witness', 'broker_witness') and
                     (event['epoch'] == term_obs['epoch'] or event['epoch'] is None and not epoch_seen),
                     'BPF terminal observer chain or boot differs')
        if event['epoch'] is not None:
            epoch_seen = True
        if event['kind'] in ('reader_witness', 'broker_witness'):
            witness = event['body']
            schemas = {
                'reader_witness_arm': 'version boot_id kind request_id nonce variant case_sha256',
                'reader_witness_chunk': 'version boot_id kind variant request_id nonce role page_index page_count total_bytes total_sha256 data_b64',
                'reader_witness_event': 'version boot_id kind variant request_id nonce check request_sha256 request_bytes original_reply_sha256 original_reply_bytes sent_reply_sha256 sent_reply_bytes send_result sent_rights endpoint_closed queries',
                'reader_pidfd_exit': 'kind variant reader_pid pidfd_exited query_dup_closed_after_exit',
                'broker_witness_chunk': 'version boot_id kind variant request_id nonce role page_index page_count total_bytes total_sha256 data_b64',
                'broker_credential_send': 'version boot_id kind variant request_id nonce input_sha256 input_bytes send_result',
                'broker_receive_observation': 'version boot_id kind variant source detector request_id nonce input_sha256 input_bytes original_sha256 expected_actor observed_actor deadline_ns observed_ns capacity count_before rejected_wire guard_sequence receive_observation',
            }
            kind = witness.get('kind') if type(witness) is dict else None
            host.require(kind in schemas and _shape(witness, schemas[kind].split()) and
                         (kind == 'reader_pidfd_exit' or witness['version'] == 1 and witness['boot_id'] == term_obs['boot_id']) and
                         witness['variant'] == expected_variant and
                         (event['kind'] == 'broker_witness') == kind.startswith('broker_'),
                         'BPF observer witness schema differs')
        else:
            _record_body(host, event['kind'], event['body'])
        raw = _canonical(event)
        charged += len(raw) + 1
        host.require(charged <= _MAX_TRANSCRIPT_BYTES, 'BPF terminal transcript exceeds byte limit')
        host.require(len(raw) <= 16384, 'BPF terminal observer record exceeds limit')
        previous = hashlib.sha256(b'HOUFENG-G2-BPF-OBS-V1\0' + raw).hexdigest()
        if event['kind'] == 'guard':
            guard = event['body']['record']
            host.require(guard['sequence'] > guard_sequence and
                         (guard['sequence'] == guard_sequence + 1 or losses['native'] and index > term_obs['begin_sequence']),
                         'BPF terminal native guard sequence gap or reuse')
            guard_sequence = guard['sequence']
            host.require(index > term_obs['begin_sequence'] or guard_sequence <= before['sequence'],
                         'BPF terminal ARM prefix exceeds its native status watermark')
            guards[guard_sequence] = guard
    host.require(previous == term_obs['terminal_sha256'] and guard_sequence <= after['sequence'] and
                 (losses['native'] or guard_sequence == after['sequence']),
                 'BPF terminal observer hash or native cursor differs')
    if prefix is not None:
        module = _replay_module_instance(host, actor_records, prefix)
        observers = [e['body'] for e in actor_records if e['kind'] == 'principal' and
                     e['body']['id'] == term_obs['observer_principal']]
        host.require(term_obs['boot_id'] == prefix['boot_id'] and term_obs['epoch'] == prefix['epoch'] and
                     len(observers) == 1 and observers[0]['role'] == 'BOOTSTRAP_TERMINAL_OBSERVER' and
                     observers[0]['task_cookie'] == after['observer_cookie'] and
                     module['instance'] == after['module_instance'] and
                     module['load_file'] == {'dev': after['module_dev'], 'ino': after['module_ino'], 'size': after['module_size']} and
                     after['observer_bindings_sha256'] == prefix['observer_bindings_sha256'] and
                     after['bootstrap_digest'] == prefix['bootstrap_manifest_sha256'] and
                     after['link_ids'] == [link['link_id'] for link in prefix['links']],
                     'BPF terminal observer or module provenance differs from bootstrap')
        objects = {e['body']['cookie']: e['body'] for e in actor_records if e['kind'] == 'object'}
        host.require(all(link['program_cookie'] in objects for link in prefix['links']) and
                     after['program_ids'] == [objects[link['program_cookie']]['kernel_id'] for link in prefix['links']],
                     'BPF terminal program IDs differ from bootstrap')
    _terminal_exit(host, term_obs['broker_exit'], guards, 15, required=not early and not losses['native'])
    if early:
        host.require(term_obs['broker_exit'] is not None and term_obs['broker_exit']['direct_wait'] is not None and
                     term_obs['broker_exit']['guard_exit_code'] == 9 and after['state'] == 'dead' and
                     after['authority_phase'] == 0, 'BPF early broker lacks its actual parent wait and owned exit')
    for item in term_obs['probe_exits']:
        _terminal_exit(host, item, guards, 11, required=not losses['native'])
    if not early:
        _replay_raw_evidence(host, records, actor_records, fault=losses if term_obs['outcome'] == 'closure_on_fault' else None,
                             begin=term_obs['begin_sequence'])
    query_sequence, port, last = 0, None, None
    for event in records:
        if event['kind'] == 'audit_readback':
            body = event['body']
            status, rules, query_sequence, port = _audit_queries(host, body, query_sequence, port)
            last = (event['sequence'], body, status, rules)
    if not early and not losses['audit']:
        host.require(last is not None and last[0] > term_obs['begin_sequence'] and
                     last[1]['reader_cookie'] == after['observer_cookie'] and last[1]['check'] == 'configuration' and
                     last[2][1] == 2 and last[2][2] == 1 and last[2][4:7] == (0, 16384, 0) and
                     set(last[3]) == set(_audit_rules()), 'BPF terminal lacks a fresh locked audit GET/LIST/GET')
    return guards


def _authorization_bounds(host, item, records, hashes):
    bounds, releases = [], []
    for field in ('before', 'after'):
        bound = item[field]
        host.require(_shape(bound, ('checkpoint_sequence', 'tail_sha256', 'guard_sequence')) and
                     _integer(bound['checkpoint_sequence'], 1, len(records)) and _hex(bound['tail_sha256']) and
                     _integer(bound['guard_sequence']) and records[bound['checkpoint_sequence'] - 1]['kind'] == 'checkpoint',
                     'BPF authorization checkpoint reference invalid')
        event = records[bound['checkpoint_sequence'] - 1]
        checkpoint = event['body']
        matching = [candidate['sequence'] for candidate in records if candidate['kind'] == 'checkpoint' and
                    candidate['body']['state'] == 'released' and candidate['sequence'] >= event['sequence'] and
                    all(candidate['body'][key] == checkpoint[key] for key in
                        ('nonce', 'principal_id', 'begin_watermark', 'tail_before'))]
        host.require(checkpoint['state'] == 'quiesced' and len(matching) == 1 and
                     bound['tail_sha256'] == hashes[bound['checkpoint_sequence']] and
                     bound['guard_sequence'] == checkpoint['end_watermark'],
                     'BPF authorization checkpoint lacks its matching successful release')
        bounds.append(bound['checkpoint_sequence'])
        releases.append(matching[0])
    host.require(bounds[0] < bounds[1] and type(item['evidence_sequences']) is list and
                 all(_integer(seq, bounds[0] + 1, bounds[1] - 1) for seq in item['evidence_sequences']) and
                 item['evidence_sequences'] == sorted(set(item['evidence_sequences'])),
                 'BPF authorization evidence interval invalid')
    host.require(bounds[0] < releases[0] < bounds[1] < releases[1] and not any(
                 e['kind'] == 'checkpoint' and e['body']['state'] == 'quiesced'
                 for e in records[bounds[1]:releases[1] - 1]),
                 'BPF authorization after-checkpoint release is outside its own interval')
    return bounds


def _verify_id_reuse(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == 'id-reuse']
    host.require([c['phase'] for c in cases] == ['started', 'observed', 'settled'] and
                 len({c['nonce'] for c in cases}) == 1 and len(cases[-1]['transaction_ids']) == 2,
                 'BPF ID reuse case phases or transactions differ')
    txids = cases[-1]['transaction_ids']
    host.require(txids[0] != txids[1], 'BPF ID reuse transaction reused')
    guards = [e['body']['record'] for e in events if e['kind'] == 'guard']
    pubs = [g for g in guards if g['kind'] == 3 and
            g['object_snapshot']['attributes']['name'] == 'g2_id_reuse']
    host.require(len(pubs) == 2 and pubs[0]['kernel_id'] == pubs[1]['kernel_id'] and
                 pubs[0]['object_cookie'] != pubs[1]['object_cookie'], 'BPF reused map identity differs')
    for pub in pubs:
        a = pub['object_snapshot']['attributes']
        host.require(pub['object_snapshot']['type'] == 2 and
                     (a['key_size'], a['value_size'], a['max_entries'], a['map_flags']) == (4, 4, 1, 0),
                     'BPF reused map creation shape differs')
    removals = [[g for g in guards if g['kind'] == 4 and g['object_cookie'] == pub['object_cookie'] and
                 g['kernel_id'] == pub['kernel_id']] for pub in pubs]
    cursors = [g for g in guards if g['kind'] == 16]
    host.require(all(len(found) == 1 for found in removals) and len(cursors) == 3 and
                 [g['operation'] for g in cursors] == [1, 2, 3], 'BPF ID reuse removals or cursor phases missing')
    first_removal, second_removal = removals[0][0], removals[1][0]
    host.require(pubs[0]['sequence'] < first_removal['sequence'] < cursors[0]['sequence'] <
                 cursors[1]['sequence'] < cursors[2]['sequence'] < pubs[1]['sequence'] < second_removal['sequence'] and
                 all(g['object_cookie'] == pubs[0]['object_cookie'] and g['kernel_id'] == pubs[0]['kernel_id'] and
                     g['detail']['first_removal_sequence'] == first_removal['sequence'] and
                     g['detail']['transaction_id'] == txids[1] for g in cursors) and
                 cursors[1]['detail']['saved_cursor'] == cursors[2]['detail']['saved_cursor'] == cursors[2]['detail']['cursor_after'] and
                 cursors[2]['detail']['allocator_result'] == pubs[0]['kernel_id'],
                 'BPF ID reuse cursor sequence or restoration differs')
    for txid, pub, removal in zip(txids, pubs, (first_removal, second_removal)):
        txns = [e['body'] for e in events if e['kind'] == 'transaction' and e['body']['id'] == txid]
        objs = [e['body'] for e in events if e['kind'] == 'object' and e['body']['cookie'] == pub['object_cookie']]
        audits = [e['body'] for e in events if e['kind'] == 'audit' and e['body']['record_type'] == 'SYSCALL' and
                  e['body']['transaction_id'] == txid]
        host.require(txns and txns[-1]['state'] == 'retired' and txns[-1]['command'] == 'MAP_CREATE' and
                     txns[-1]['cleanup']['state'] == 'settled' and objs and
                     objs[-1]['owner_transaction'] == txid and objs[-1]['removal_seq'] == removal['sequence'] and
                     len(audits) == 1 and audits[0]['command'] == 'MAP_CREATE',
                     'BPF ID reuse lacks actual transaction, cleanup, object or syscall audit')


def _verify_control_case(host, item, records):
    name = item['name']
    host.require(name in ('wrong-generation', 'fd-arm-replay', 'fd-arm-wrong-role', 'fd-arm-wrong-grant', 'fd-arm-second'),
                 'BPF unknown control case')
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    controls = [e for e in events if e['kind'] == 'control_rejection']
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == name]
    host.require([c['phase'] for c in cases] == ['started', 'observed', 'settled'] and
                 len({c['nonce'] for c in cases}) == 1 and controls,
                 'BPF control case lacks independent phases')
    principals = {e['body']['id']: e['body'] for e in records if e['kind'] == 'principal'}
    for event in controls:
        c = event['body']
        host.require(item['before']['guard_sequence'] <= c['guard_before'] <= c['guard_after'] <= item['after']['guard_sequence'],
                     'BPF control guard bounds differ')
        request = {'version': 1, 'boot_id': event['boot_id'], 'request_id': c['request_id'],
                   'operation': c['operation'], 'body': c['body']}
        if c['operation'] != 'BIND_RUNTIME':
            host.require(c['principal_id'] in principals, 'BPF control principal missing')
            request.update(epoch=event['epoch'], run_token=host.TOKEN, principal_id=c['principal_id'])
        host.require(hashlib.sha256(_canonical(request)).hexdigest() == c['request_sha256'],
                     'BPF control request digest differs from its actual wire envelope')
    denied = [e for e in controls if e['body']['status'] == 'BPF_AUTH_DENIED' and e['body']['code'] == 'BPF_AUTH_DENIED']
    host.require(denied and item['control_error'] == 'BPF_AUTH_DENIED', 'BPF control denial missing')
    event = denied[-1]
    denial, body = event['body'], event['body']['body']
    host.require(any(c['request_sha256'] == denial['request_sha256'] and c['error_code'] == 'BPF_AUTH_DENIED' for c in cases),
                 'BPF control case does not reference its actual rejection')
    if denial['principal_id'] is not None:
        principal = principals[denial['principal_id']]
        host.require(all(principal[k] == item['actor'][k] for k in ('tgid', 'tid', 'task_cookie', 'start_ns')),
                     'BPF control requester differs from observed actor')
    else:
        actor = item['actor']
        lineage = [e['body']['record'] for e in records[:event['sequence']] if e['kind'] == 'guard' and
                   e['body']['record']['kind'] in (9, 10, 11, 12) and
                   e['body']['record']['task_cookie'] == actor['task_cookie']]
        host.require(any(c['actor_cookie'] == actor['task_cookie'] and c['request_sha256'] == denial['request_sha256']
                         for c in cases) and lineage and all(
                         (g['detail']['tgid'], g['detail']['tid'], g['task_start_ns']) ==
                         (actor['tgid'], actor['tid'], actor['start_ns']) for g in lineage),
                     'BPF preprincipal requester lacks native lineage identity')
    if name == 'wrong-generation':
        grants = [e['body'] for e in records[:event['sequence'] - 1] if e['kind'] == 'grant' and
                  e['body']['container_id'] == body.get('container_id')]
        started = next(e['sequence'] for e in events if e['kind'] == 'selftest_case' and
                       e['body']['name'] == name and e['body']['phase'] == 'started')
        host.require(denial['operation'] == 'BIND_RUNTIME' and denial['principal_id'] is None and denial['nonce'] is None and
                     _shape(body, ('container_id', 'command', 'bundle_sha256', 'process_sha256')) and
                     body['command'] in ('create', 'start') and grants and grants[-1]['phase'] == 'retired' and
                     not any(e['sequence'] > started and (e['kind'] == 'invocation' or
                             e['kind'] == 'transaction' and e['body']['state'] in ('committed', 'effect_observed') or
                             e['kind'] == 'attachment' and e['body']['state'] == 'attached' or
                             e['kind'] == 'guard' and e['body']['record']['kind'] in (1, 2, 3, 4, 5, 6)) for e in events),
                     'BPF wrong-generation request acquired an invocation or effect')
    else:
        host.require(denial['operation'] == 'SELFTEST_FD_ARM' and _shape(body, ('nonce', 'grant_id', 'class')) and
                     body['nonce'] == denial['nonce'] and _hex(body['nonce'], _HEX32) and body['class'] in ('map', 'program') and
                     _integer(body['grant_id'], 1), 'BPF denied FD arm request differs')
        if name == 'fd-arm-wrong-role':
            host.require(principals[denial['principal_id']]['role'] != 'REALM_ROOT', 'BPF wrong-role arm used root')
        elif name == 'fd-arm-wrong-grant':
            grants = [e['body'] for e in records[:event['sequence'] - 1] if e['kind'] == 'grant' and e['body']['id'] == body['grant_id']]
            earlier_arms = [e for e in records[:event['sequence'] - 1] if e['kind'] == 'selftest_fd_stage' and
                            e['body']['phase'] == 'armed']
            later_arms = [e for e in records[event['sequence']:] if e['kind'] == 'selftest_fd_stage' and
                          e['body']['phase'] == 'armed' and e['body']['class'] in ('map', 'program')]
            host.require(grants and grants[-1]['phase'] == 'retired' and
                         grants[-1]['purpose'].startswith('postgres-init-') and
                         principals[denial['principal_id']]['role'] == 'REALM_ROOT' and body['class'] == 'map' and
                         not earlier_arms and not any(e['kind'] == 'selftest_fd_stage' for e in events) and
                         len(later_arms) >= 2 and [e['body']['class'] for e in later_arms[:2]] == ['map', 'program'] and
                         all(e['body']['grant_id'] != body['grant_id'] for e in later_arms[:2]),
                         'BPF wrong-grant attempt lacked eligible unused class budgets or a retired distinct grant')
            host.require(not any(e['kind'] == 'transaction' and e['body']['state'] in ('committed', 'effect_observed') or
                                 e['kind'] == 'attachment' and e['body']['state'] == 'attached' or
                                 e['kind'] == 'guard' and e['body']['record']['kind'] in (1, 2, 3, 4, 5, 6)
                                 for e in events), 'BPF wrong-grant interval acquired an effect')
        elif name == 'fd-arm-second':
            host.require(any(e['kind'] == 'selftest_fd_stage' and e['body']['class'] == body['class'] and
                             e['body']['phase'] == 'armed' and e['sequence'] < event['sequence'] for e in records),
                         'BPF second arm lacks previously consumed class budget')
        else:
            ok = [e['body'] for e in controls if e['body']['status'] == 'OK']
            host.require(len(ok) == 2 and all(c['request_id'] == ok[0]['request_id'] and c['request_sha256'] == ok[0]['request_sha256'] and
                         c['principal_id'] == ok[0]['principal_id'] for c in ok) and
                         denial['request_id'] == ok[0]['request_id'] and denial['request_sha256'] != ok[0]['request_sha256'],
                         'BPF FD arm replay lacks cached ACK or changed request rejection')
    host.require(not any(e['kind'] == 'transaction' and e['body']['state'] in ('committed', 'effect_observed') or
                         e['kind'] == 'attachment' and e['body']['state'] == 'attached' or
                         e['kind'] == 'guard' and e['body']['record']['kind'] in (1, 2, 3, 4, 5, 6)
                         for e in events if e['sequence'] > event['sequence']),
                 'BPF rejected control request acquired a new effect')


def _verify_assembly_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    actors = [e['body'] for e in events if e['kind'] == 'probe_actor']
    parents = [a for a in actors if a['name'] == item['name']]
    imports = [e['body'] for e in events if e['kind'] == 'probe_import' and e['body']['notifying_cookie'] is not None]
    guards = [e['body']['record'] for e in events if e['kind'] == 'guard']
    host.require(len(parents) == len(imports) == 1 and item['syscall_result'] == -1 and
                 item['control_error'] is None and item['request'] is None and item['audit'] is None,
                 'BPF assembly case lacks its own probe and EPERM result')
    parent, imported = parents[0], imports[0]
    actor = item['actor']
    assembly = item['assembly']
    host.require(_shape(assembly, ('probe_cookie', 'pid', 'start_ticks', 'wait_pid', 'wait_status', 'frames')) and
                 all(_integer(assembly[k], 1) for k in ('probe_cookie', 'pid', 'start_ticks', 'wait_pid')) and
                 type(assembly['wait_status']) is int and assembly['wait_status'] == 0 and
                 assembly['wait_pid'] == assembly['pid'] == parent['tgid'] == parent['tid'] and
                 assembly['probe_cookie'] == parent['probe_cookie'] and
                 assembly['start_ticks'] == parent['start_ns'] // 10000000 and
                 type(assembly['frames']) is list and len(assembly['frames']) == (3 if item['name'] == 'tid-reuse' else 1) and
                 all(type(frame) is dict and len(_canonical(frame)) <= 16384 for frame in assembly['frames']),
                 'BPF assembly child identity, wait result or frame bounds differ')
    parent_watches = [g for g in guards if g['kind'] == 12 and g['task_cookie'] == parent['task_cookie']]
    host.require(len(parent_watches) == 1 and parent_watches[0]['task_start_ns'] == parent['start_ns'] and
                 parent_watches[0]['detail']['tgid'] == parent['tgid'] and
                 parent_watches[0]['detail']['tid'] == parent['tid'] and
                 parent_watches[0]['parent_cookie'] == parent['parent_cookie'],
                 'BPF assembly parent lacks its registered native watch')
    frames = assembly['frames']
    final = frames[-1]
    mode = item['name']
    host.require(final.get('mode') == mode and type(final.get('bpf_result')) is int and final['bpf_result'] == -1 and
                 type(final.get('dup_result')) is int and final['dup_result'] == 10,
                 'BPF assembly final syscall results differ')
    if mode == 'fd-slot-reuse':
        host.require(_shape(final, ('mode', 'tid', 'dup_result', 'bpf_result')) and
                     _integer(final['tid'], 1) and final['tid'] == assembly['pid'] == actor['tid'],
                     'BPF slot reuse frame differs')
    elif mode in ('nonleader-files', 'shared-files'):
        host.require(_shape(final, ('mode', 'leader_tid', 'tid', 'clone_result', 'unshare_result', 'dup_result',
                                    'bpf_result', 'leader_is_a', 'leader_is_b')) and
                     all(type(final[k]) is int for k in ('leader_tid', 'tid', 'clone_result', 'unshare_result')) and
                     final['leader_tid'] == assembly['pid'] and final['tid'] == final['clone_result'] == actor['tid'] and
                     final['tid'] != assembly['pid'] and final['unshare_result'] == 0 and
                     final['leader_is_a'] is (mode == 'nonleader-files') and
                     final['leader_is_b'] is (mode == 'shared-files'), 'BPF shared file frame differs')
    elif mode == 'leader-exit':
        host.require(_shape(final, ('mode', 'leader_tid', 'tid', 'barrier_result', 'dup_result', 'bpf_result')) and
                     all(type(final[k]) is int for k in ('leader_tid', 'tid', 'barrier_result')) and
                     final['leader_tid'] == assembly['pid'] and final['tid'] == actor['tid'] and
                     final['tid'] != assembly['pid'] and final['barrier_result'] == 1,
                     'BPF leader exit frame differs')
    else:
        first, second = frames[:2]
        host.require(_shape(first, ('mode', 'phase', 'first_tid')) and
                     _shape(second, ('mode', 'phase', 'first_tid', 'new_tid', 'first_wait_status')) and
                     _shape(final, ('mode', 'first_tid', 'first_wait_status', 'clone_result', 'wait_status', 'dup_result', 'bpf_result')) and
                     first['mode'] == second['mode'] == mode and first['phase'] == 'first-child-started' and
                     second['phase'] == 'reused-child-started' and
                     all(type(value) is int for value in (first['first_tid'], second['first_tid'], second['new_tid'],
                         second['first_wait_status'], final['first_tid'], final['first_wait_status'], final['clone_result'], final['wait_status'])) and
                     first['first_tid'] == second['first_tid'] == second['new_tid'] == final['first_tid'] == final['clone_result'] == actor['tid'] and
                     second['first_wait_status'] == final['first_wait_status'] == final['wait_status'] == 0,
                     'BPF TID reuse frames differ')
    host.require(imported['notifying_cookie'] == actor['task_cookie'] and imported['slot'] == 10 and
                 imported['fixture'] == 'b' and imported['result'] == 'imported' and
                 _integer(imported['imported_device'], 1) and _integer(imported['imported_inode'], 1) and
                 item['before']['guard_sequence'] <= imported['guard_begin'] <= imported['guard_end'] <= item['after']['guard_sequence'],
                 'BPF assembly import is not the actual fixture B notification')
    lineage = [g for g in guards if g['kind'] in (9, 10, 11, 12) and g['task_cookie'] == actor['task_cookie']]
    host.require(lineage and all((g['detail']['tgid'], g['detail']['tid'], g['task_start_ns']) ==
                                 (actor['tgid'], actor['tid'], actor['start_ns']) for g in lineage),
                 'BPF assembly notifying task lineage differs')
    if item['name'] == 'tid-reuse':
        children = [a for a in actors if a['name'] == 'tid-reuse-child' and
                    a['parent_cookie'] == parent['task_cookie'] and a['listener_id'] == parent['listener_id']]
        host.require(len(children) == 2 and children[0]['tid'] == children[1]['tid'] == actor['tid'] and
                     children[0]['task_cookie'] != children[1]['task_cookie'] == imported['notifying_cookie'] and
                     children[0]['start_ns'] != children[1]['start_ns'] and
                     imported['probe_cookie'] == children[1]['probe_cookie'] and any(
                         g['kind'] == 11 and g['task_cookie'] == children[0]['task_cookie'] for g in guards),
                     'BPF TID reuse lacks distinct old and new task incarnations')
    else:
        host.require(imported['probe_cookie'] == parent['probe_cookie'] and actor['tgid'] == parent['tgid'],
                     'BPF assembly import is not bound to its registered listener')
        if item['name'] == 'fd-slot-reuse':
            host.require(actor['task_cookie'] == parent['task_cookie'] and actor['tid'] == parent['tid'],
                         'BPF slot reuse did not notify from the probe leader')
        else:
            host.require(actor['tid'] != parent['tid'] and any(g['kind'] == 9 for g in lineage),
                         'BPF assembly notification did not originate in a child thread')
            if item['name'] == 'leader-exit':
                host.require(any(g['kind'] == 11 and g['task_cookie'] == parent['task_cookie'] and
                                 g['sequence'] <= imported['guard_end'] for g in guards),
                             'BPF leader did not exit before notification')


def _verify_interleaved_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    probes = [e['body'] for e in events if e['kind'] == 'probe_actor' and e['body']['name'] == 'raw-denial']
    host.require(len(probes) == 2 and probes[0]['task_cookie'] != probes[1]['task_cookie'],
                 'BPF interleaved audit lacks two distinct registered actors')
    audits = []
    for probe in probes:
        found = [e['body'] for e in events if e['kind'] == 'audit' and e['body']['raw'] is not None and
                 e['body']['task_cookie'] == probe['task_cookie'] and e['body']['command'] == 'MAP_CREATE' and
                 e['body']['record_type'] == 'SYSCALL' and e['body']['result'] == -1]
        host.require(len(found) == 1, 'BPF interleaved actor lacks its denied map syscall')
        audits.append(found[0])
    stamps = [_audit_stamp(host, audit) for audit in audits]
    host.require(stamps[0] != stamps[1] and audits[0]['serial'] != audits[1]['serial'],
                 'BPF interleaved actors reused an audit serial')
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    selected = [e['body']['record'] for e in events if e['kind'] == 'guard' and
                e['body']['record']['kind'] == 13 and e['body']['record']['operation'] == 0 and
                e['body']['record']['task_cookie'] in {p['task_cookie'] for p in probes}]
    host.require(len(cases) == 3 and len(selected) == 2 and
                 set(cases[-1]['guard_sequences']) == {g['sequence'] for g in selected} and
                 cases[-1]['actor_cookie'] in {p['task_cookie'] for p in probes} and
                 item['syscall_result'] == cases[-1]['result'] == -1,
                 'BPF interleaved case is not bound to both native denials')
    witnesses = [e for e in records if e['kind'] == 'selftest_audit_reorder']
    case_events = [e for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    host.require(len(witnesses) == 1 and witnesses[0] in events and len(case_events) == 3 and
                 case_events[0]['sequence'] < witnesses[0]['sequence'] < case_events[-1]['sequence'],
                 'BPF audit reorder witness is not within the active case')
    witness = witnesses[0]['body']
    _record_body(host, 'selftest_audit_reorder', witness)
    host.require(witness['nonce'] == cases[0]['nonce'] == cases[-1]['nonce'],
                 'BPF audit reorder witness nonce differs from active STEP')
    expected = []
    for typ in (1300, 1320):
        for audit in audits:
            raw = audit['raw']
            prefix = 'syscall' if typ == 1300 else 'eoe'
            expected.append({'stamp': audit['stamp'], 'serial': audit['serial'], 'record_type': typ,
                             'payload_sha256': raw[prefix + '_payload_sha256'],
                             'occurrence': raw[prefix + '_occurrence']})
    received = witness['received']
    delivered = [received[index - 1] for index in witness['delivery_order']]
    wires = [e for e in events if e['kind'] == 'audit_wire' and
             _audit_stamp(host, e['body']) in stamps and e['body']['record_type'] in (1300, 1320)]
    host.require(delivered == expected and [e['body'] for e in wires] == delivered and
                 all(e['sequence'] > witnesses[0]['sequence'] for e in wires),
                 'BPF audit receipt permutation or actual delivery differs')
    _replay_raw_evidence(host, events, records)


def _verify_extra_fd_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    host.require([e['body']['phase'] for e in cases] == ['started', 'observed', 'settled'] and
                 len({e['body']['nonce'] for e in cases}) == 1 and
                 len(cases[-1]['body']['reference_cookies']) == 1,
                 'BPF extra descriptor case lacks its ordered object identity')
    case = cases[-1]['body']
    cookie = case['reference_cookies'][0]
    transfers = [e for e in events if e['kind'] == 'selftest_runtime_stage' and
                 e['body']['mode'] == item['name'] and e['body']['phase'] == 'injected']
    probes = [e for e in events if e['kind'] == 'probe_actor' and e['body']['name'] == item['name']]
    host.require(len(transfers) == len(probes) == 1, 'BPF extra descriptor injection or actor missing')
    transfer, probe = transfers[0]['body'], probes[0]['body']
    grant = transfer['grant_id']
    host.require(transfer['nonce'] == case['nonce'] and transfer['invocation_id'] == case['invocation_id'] and
                 transfer['notifying_cookie'] == probe['task_cookie'] == item['actor']['task_cookie'] == case['actor_cookie'] and
                 transfer['mutation']['listener_id'] == probe['listener_id'] and
                 transfer['mutation']['program_cookie'] == cookie and _integer(transfer['notification_id'], 1) and
                 _integer(transfer['mutation']['installed_fd'], high=(1 << 31) - 1) and
                 cases[1]['body']['actor_cookie'] == probe['task_cookie'] and
                 cases[1]['body']['invocation_id'] == case['invocation_id'] and
                 cases[1]['body']['reference_cookies'] == [cookie],
                 'BPF extra descriptor transfer differs from its case, listener or actor')
    invocations = [e['body'] for e in events if e['kind'] == 'invocation' and e['body']['id'] == case['invocation_id']]
    host.require(invocations and all(inv['grant_id'] == grant for inv in invocations),
                 'BPF extra descriptor invocation belongs to another grant')
    objects = [e for e in events if e['kind'] == 'object' and e['body']['cookie'] == cookie]
    closes = [e for e in events if e['kind'] == 'grant_object_cleanup' and e['body']['object_cookie'] == cookie]
    removals = [e for e in events if e['kind'] == 'guard' and e['body']['record']['kind'] == 2 and
                e['body']['record']['object_cookie'] == cookie]
    host.require(objects and [e['body']['step'] for e in closes] == [1, 2] and len(removals) == 1,
                 'BPF extra descriptor lacks exact close, removal and cleanup')
    final = objects[-1]['body']
    host.require(final['class'] == 'program' and final['bytes'] == _device_program().hex() and
                 final['state'] == 'id_removed' and final['removal_seq'] == removals[0]['body']['record']['sequence'] and
                 final['kernel_id'] == removals[0]['body']['record']['kernel_id'] and
                 all(c['body']['grant_id'] == grant and c['body']['creator_transaction_id'] == final['owner_transaction']
                     for c in closes) and closes[0]['body']['result'] == 0 and
                 closes[1]['body']['guard_sequence'] == final['removal_seq'] and
                 closes[1]['body']['result'] == removals[0]['body']['record']['result'],
                 'BPF extra descriptor final program or cleanup identity differs')
    attached = [e for e in events if e['kind'] == 'attachment' and e['body']['grant_id'] == grant and
                e['body']['program_cookie'] == cookie]
    detached = [e for e in attached if e['body']['state'] == 'detached']
    txids = set(case['transaction_ids'])
    detaches = [e for e in events if e['kind'] == 'transaction' and e['body']['id'] in txids and
                e['body']['grant_id'] == grant and e['body']['command'] == 'PROG_DETACH' and
                e['body']['state'] == 'retired' and e['body']['result'] == 0 and
                any(e['body']['output'] == {'attachment_id': a['body']['id']} for a in detached)]
    host.require(attached and attached[0]['body']['state'] == 'attached' and len(detached) == len(detaches) == 1 and
                 final['owner_transaction'] in txids and attached[0]['body']['transaction_id'] in txids and
                 set(cases[1]['body']['transaction_ids']) == txids and
                 transfers[0]['sequence'] < detached[0]['sequence'] < detaches[0]['sequence'] < cases[1]['sequence'] and
                 transfers[0]['sequence'] < closes[0]['sequence'] < cases[1]['sequence'],
                 'BPF extra descriptor lacks the case-bound actual detach and broker close')
    grants = [e for e in events if e['kind'] == 'grant' and e['body']['id'] == grant]
    retirements = [e for e in grants if e['body']['phase'] == 'retired']
    host.require(len(retirements) == 1, 'BPF extra descriptor grant did not retire exactly once')
    owner_id = retirements[0]['body']['owner']
    owners = [e['body'] for e in records if e['kind'] == 'principal' and e['body']['id'] == owner_id]
    host.require(owners and owners[-1]['role'] == 'REALM_ROOT' and probe['parent_cookie'] == owners[-1]['task_cookie'],
                 'BPF extra descriptor probe is not owned by the grant principal')
    watches = [e for e in events if e['kind'] == 'guard' and e['body']['record']['kind'] == 12 and
               e['body']['record']['task_cookie'] == probe['task_cookie']]
    brokers = [e['body'] for e in records if e['kind'] == 'principal' and e['body']['role'] == 'BOOTSTRAP_BROKER']
    host.require(len(watches) == 1 and watches[0]['sequence'] < transfers[0]['sequence'] and
                 watches[0]['body']['record']['task_start_ns'] == probe['start_ns'] and
                 watches[0]['body']['record']['parent_cookie'] == probe['parent_cookie'] and
                 watches[0]['body']['record']['detail']['tgid'] == probe['tgid'] == probe['tid'] and
                 watches[0]['body']['record']['detail']['tid'] == probe['tid'] and brokers and
                 all(b['exe_sha256'] == probe['exe_sha256'] for b in brokers),
                 'BPF extra descriptor actor lacks its actual watched child incarnation')
    checkpoints = [e for e in events if e['kind'] == 'checkpoint' and e['body']['state'] == 'quiesced']
    def held_at(checkpoint):
        prior = [o for o in objects if o['sequence'] < checkpoint['sequence']]
        prior_grants = [g for g in grants if g['sequence'] < checkpoint['sequence']]
        releases = [e for e in events if e['kind'] == 'checkpoint' and e['body']['state'] == 'released' and
                    e['sequence'] > checkpoint['sequence'] and all(e['body'][k] == checkpoint['body'][k] for k in
                    ('nonce', 'principal_id', 'begin_watermark', 'tail_before'))]
        return (prior and prior[-1]['body']['state'] == 'live' and prior[-1]['body']['removal_seq'] is None and
                prior_grants and prior_grants[-1]['body']['phase'] in ('admitted', 'active') and
                checkpoint['body']['principal_id'] == owner_id and len(releases) == 1 and
                cases[1]['sequence'] < checkpoint['sequence'] < releases[0]['sequence'] < removals[0]['sequence'] <
                closes[1]['sequence'] < retirements[0]['sequence'] and
                not any(g['body']['phase'] in ('settling', 'retired') and g['sequence'] < releases[0]['sequence'] for g in grants))
    host.require(any(held_at(c) for c in checkpoints), 'BPF extra descriptor lacked its released live-program checkpoint')
    host.require(any(e['kind'] == 'checkpoint' and e['body']['state'] == 'quiesced' and
                     e['body']['principal_id'] == owner_id and e['sequence'] > retirements[0]['sequence']
                     for e in records), 'BPF extra descriptor lacks its final checkpoint')
    if 'extra_fd' in item:
        evidence = item['extra_fd']
        host.require(_shape(evidence, ('pid', 'start_ticks', 'probe_cookie', 'status', 'frames', 'intermediate', 'program_cookie')) and
                     evidence['pid'] == probe['tid'] == item['actor']['tid'] and
                     evidence['start_ticks'] == item['actor']['start_ticks'] and evidence['probe_cookie'] == probe['probe_cookie'] and
                     evidence['status'] == 0 and evidence['program_cookie'] == cookie and
                     type(evidence['frames']) is list and len(evidence['frames']) == 2,
                     'BPF extra descriptor child result differs from registered actor')
        held, closed = evidence['frames']
        host.require(held == {'mode': item['name'], 'phase': 'held', 'tid': probe['tid'], 'fd': transfer['mutation']['installed_fd']} and
                     closed == {'mode': item['name'], 'phase': 'closed', 'tid': probe['tid'],
                                'fd': transfer['mutation']['installed_fd'], 'close_result': 0},
                     'BPF extra descriptor child did not close the actually installed descriptor')
        bound = evidence['intermediate']
        host.require(_shape(bound, ('checkpoint_sequence', 'tail_sha256', 'guard_sequence')) and
                     _integer(bound['checkpoint_sequence'], 1, len(records) - 1),
                     'BPF extra descriptor intermediate checkpoint identity invalid')
        checkpoint = records[bound['checkpoint_sequence'] - 1]
        host.require(checkpoint in checkpoints and held_at(checkpoint) and
                     bound['guard_sequence'] == checkpoint['body']['end_watermark'] and
                     bound['tail_sha256'] == records[bound['checkpoint_sequence']]['previous'] and
                     item['after']['checkpoint_sequence'] > retirements[0]['sequence'],
                     'BPF extra descriptor checkpoint bounds differ from recorded lifecycle')
        exits = [e for e in events if e['kind'] == 'guard' and e['body']['record']['kind'] == 11 and
                 e['body']['record']['task_cookie'] == probe['task_cookie'] and
                 e['body']['record']['task_start_ns'] == probe['start_ns']]
        host.require(len(exits) == 1 and checkpoint['sequence'] < exits[0]['sequence'] < item['after']['checkpoint_sequence'],
                     'BPF extra descriptor child exit was not observed by the final checkpoint')


def _verify_program_cleanup_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    case = cases[-1]
    txids = set(case['transaction_ids'])
    transactions = {e['body']['id']: e['body'] for e in events if e['kind'] == 'transaction' and
                    e['body']['id'] in txids}
    invocations = [e['body'] for e in events if e['kind'] == 'invocation' and e['body']['id'] == case['invocation_id']]
    host.require(invocations and txids and txids <= transactions.keys(), 'BPF cleanup lacks its selected invocation and transactions')
    grant = invocations[0]['grant_id']
    host.require(all(t['grant_id'] == grant and (t['invocation_id'] == case['invocation_id'] or
                     item['name'] == 'attachment-replace' and t['invocation_id'] is None and
                     t['requester_cookie'] is None and t['notification_id'] is None)
                     for t in transactions.values()),
                 'BPF cleanup transactions differ from the selected grant')
    objects = {e['body']['cookie']: e['body'] for e in events if e['kind'] == 'object' and
               e['body']['cookie'] in case['reference_cookies']}
    attachments = [e['body'] for e in events if e['kind'] == 'attachment' and e['body']['grant_id'] == grant]
    if item['name'] == 'failure-cleanup':
        loads = [t for t in transactions.values() if t['command'] == 'PROG_LOAD' and t['program_bytes'] == _device_program().hex()]
        host.require(len(loads) == len(objects) == 1 and not any(a['state'] == 'attached' for a in attachments),
                     'BPF failed create lacks a device publication or unexpectedly attached')
        for obj in objects.values():
            tx = transactions[obj['owner_transaction']]
            responses = [e['body'] for e in events if e['kind'] == 'notification_response' and
                         e['body']['transaction_id'] == tx['id'] and e['body']['operation'] == 'ADDFD_SEND']
            cleanup = tx['cleanup']
            steps = cleanup['steps']
            guards = [e['body']['record'] for e in events if e['kind'] == 'guard' and
                      e['body']['record']['sequence'] in (obj['publication_seq'], obj['removal_seq'])]
            host.require(item['syscall_result'] == case['result'] == -2 and
                         _integer(tx['result'], 0) and len(responses) == 1 and responses[0]['ioctl_errno'] == 2 and
                         responses[0]['invocation_id'] == tx['invocation_id'] and
                         responses[0]['notification_id'] == tx['notification_id'] and
                         obj['class'] == 'program' and obj['state'] == 'id_removed' and obj['removal_seq'] is not None and
                         tx['state'] == 'retired' and cleanup['policy'] == 'close_unattached' and
                         cleanup['state'] == 'settled' and [s['action'] for s in steps] == ['close_ref', 'await_id_removed'] and
                         all(s['object_cookie'] == obj['cookie'] for s in steps) and
                         steps[0]['result'] == 0 and steps[1]['result'] is None and
                         steps[-1]['guard_sequence'] == obj['removal_seq'] and
                         case['guard_sequences'] == [obj['publication_seq'], obj['removal_seq']] and
                         [g['kind'] for g in guards] == [1, 2] and
                         all(g['object_cookie'] == obj['cookie'] and g['kernel_id'] == obj['kernel_id'] for g in guards) and
                         obj['publication_seq'] < obj['removal_seq'],
                         'BPF failed create lacks authorized close and matching native removal')
    else:
        loads = [t for t in transactions.values() if t['command'] == 'PROG_LOAD' and t['program_bytes'] == _device_program().hex()]
        host.require(len(loads) == 2 and len(objects) == 2 and all(o['class'] == 'program' and
                     o['owner_transaction'] in {t['id'] for t in loads} and o['state'] == 'id_removed' and
                     o['removal_seq'] is not None for o in objects.values()),
                     'BPF replacement lacks both device programs and removals')
        attached = [a for a in attachments if a['state'] == 'attached']
        host.require(len(attached) == 2 and [a['flags'] for a in attached] == [2, 6] and
                     attached[1]['replaces'] == attached[0]['id'] and
                     all(attached[0][k] == attached[1][k] for k in ('grant_id', 'enrollment', 'root_cookie', 'cgroup_id', 'attach_type')) and
                     {a['program_cookie'] for a in attached} == set(objects),
                     'BPF replacement does not replace the same cgroup attachment')
        host.require(any(a['id'] == attached[0]['id'] and a['state'] in ('detached', 'released') for a in attachments) and
                     any(a['id'] == attached[1]['id'] and a['state'] == 'detached' for a in attachments),
                     'BPF replacement attachments did not retire and detach')


def _verify_seal_detach_case(host, item, records, prefix):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    statuses = [e for e in events if e['kind'] == 'selftest_guard_status' and e['body']['name'] == item['name']]
    probes = [e['body'] for e in events if e['kind'] == 'probe_actor' and e['body']['name'] == item['name'] and
              e['body']['task_cookie'] == item['actor']['task_cookie']]
    raw = [e for e in events if e['kind'] == 'guard' and e['body']['record']['kind'] == 13 and
           e['body']['record']['task_cookie'] == item['actor']['task_cookie']]
    host.require(len(cases) == 3 and len(statuses) == 2 and len(probes) == len(raw) == 1 and
                 [e['body']['phase'] for e in statuses] == ['before', 'after'] and
                 all(e['body']['nonce'] == cases[0]['body']['nonce'] for e in statuses) and
                 statuses[0]['sequence'] < raw[0]['sequence'] < statuses[1]['sequence'] and
                 (raw[0]['body']['record']['operation'], raw[0]['body']['record']['result']) == (34, -1) and
                 item['syscall_result'] == cases[-1]['body']['result'] == -1 and
                 raw[0]['body']['record']['sequence'] in cases[-1]['body']['guard_sequences'],
                 'BPF sealed link detach lacks its actual denied call')
    module = _replay_module_instance(host, records, prefix)
    ids = [link['link_id'] for link in prefix['links']]
    host.require(all(e['body']['status']['module_instance'] == module['instance'] and
                     e['body']['status']['link_ids'] == ids and e['body']['status']['link_attached'] == [True, True, True]
                     for e in statuses), 'BPF sealed link identities changed after detach')
    _replay_raw_evidence(host, events, records)


def _verify_seal_unload_case(host, item, records, prefix):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == 'seal-unload-denied']
    statuses = [e for e in events if e['kind'] == 'selftest_guard_status' and e['body']['name'] == 'seal-unload-denied']
    probes = [e['body'] for e in events if e['kind'] == 'probe_actor' and
              e['body']['name'] == 'seal-unload-denied' and e['body']['task_cookie'] == item['actor']['task_cookie']]
    host.require(len(cases) == 3 and len(statuses) == 2 and len(probes) == 1 and
                 [e['body']['phase'] for e in statuses] == ['before', 'after'] and
                 all(e['body']['nonce'] == cases[0]['body']['nonce'] for e in statuses) and
                 cases[0]['sequence'] < statuses[0]['sequence'] < statuses[1]['sequence'] < cases[1]['sequence'] and
                 _integer(item['syscall_result'], _I64_MIN, -1) and item['syscall_result'] != -2 and
                 cases[-1]['body']['result'] == item['syscall_result'],
                 'BPF seal unload case lacks its real denial and status sequence')
    module = _replay_module_instance(host, records, prefix)
    expected_ids = [link['link_id'] for link in prefix['links']]
    host.require(all(e['body']['status']['module_instance'] == module['instance'] and
                     e['body']['status']['authority_phase'] == 1 and e['body']['status']['link_ids'] == expected_ids and
                     e['body']['status']['link_attached'] == [True, True, True] for e in statuses),
                 'BPF seal unload changed the original module or held links')
    exit_guards = [e for e in events if e['kind'] == 'guard' and e['body']['record']['kind'] == 11 and
                   e['body']['record']['task_cookie'] == probes[0]['task_cookie']]
    host.require(len(exit_guards) == 1 and statuses[0]['sequence'] < exit_guards[0]['sequence'] < statuses[1]['sequence'] and
                 exit_guards[0]['body']['record']['sequence'] in cases[-1]['body']['guard_sequences'] and
                 exit_guards[0]['body']['record']['task_start_ns'] == probes[0]['start_ns'] and
                 exit_guards[0]['body']['record']['detail']['exit_code'] == 0,
                 'BPF seal unload lacks the authenticated child exit')


def _verify_container_probe_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == 'container-bpf']
    actors = [e['body'] for e in events if e['kind'] == 'probe_actor' and
              e['body']['name'] == 'container-raw-denial' and e['body']['task_cookie'] == item['actor']['task_cookie']]
    raw = [e['body']['record'] for e in events if e['kind'] == 'guard' and
           e['body']['record']['kind'] == 13 and e['body']['record']['task_cookie'] == item['actor']['task_cookie']]
    host.require(len(cases) == 3 and len(actors) == 1 and item['syscall_result'] == -1 and
                 [(g['operation'], g['result']) for g in raw] == [(0, -1), (5, -1)] and
                 cases[-1]['guard_sequences'] == [g['sequence'] for g in raw],
                 'BPF container probe lacks its two actual denied calls')
    host.require(not any(e['kind'] == 'guard' and e['body']['record']['task_cookie'] == item['actor']['task_cookie'] and
                         e['body']['record']['kind'] in (1, 2, 3, 4, 5, 6) for e in events),
                 'BPF container raw denial produced a native effect')
    _replay_raw_evidence(host, events, records)


def _verify_device_lifecycle_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    host.require([c['phase'] for c in cases] == ['started', 'observed', 'settled'] and
                 len({c['nonce'] for c in cases}) == 1 and len(cases[-1]['transaction_ids']) == 3 and
                 len(cases[-1]['reference_cookies']) == 1 and
                 cases[-1]['actor_cookie'] == item['actor']['task_cookie'] and
                 item['syscall_result'] == cases[-1]['result'] == 0,
                 'BPF device lifecycle case index differs')
    txids, cookie = cases[-1]['transaction_ids'], cases[-1]['reference_cookies'][0]
    txns = []
    for txid in txids:
        versions = [e['body'] for e in events if e['kind'] == 'transaction' and e['body']['id'] == txid]
        host.require(versions and versions[-1]['state'] == 'retired' and
                     (txid == txids[2] or versions[-1]['invocation_id'] == cases[-1]['invocation_id']),
                     'BPF device lifecycle transaction did not retire')
        txns.append(versions[-1])
    load, attach, query = txns
    host.require([t['command'] for t in txns] == ['PROG_LOAD', 'PROG_ATTACH', 'PROG_QUERY'] and
                 load['requester_cookie'] == item['actor']['task_cookie'] and
                 load['program_bytes'] == _device_program().hex() and attach['result'] == query['result'] == 0,
                 'BPF device load, attach or query differs')
    objects = [e['body'] for e in events if e['kind'] == 'object' and e['body']['cookie'] == cookie]
    host.require(objects and objects[-1]['owner_transaction'] == load['id'] and objects[-1]['state'] == 'id_removed' and
                 objects[-1]['removal_seq'] is not None and
                 query['output'] == {'attach_flags': 2, 'program_ids': [objects[-1]['kernel_id']], 'program_attach_flags': [2]},
                 'BPF device query or final program removal differs')
    attachments = [e['body'] for e in events if e['kind'] == 'attachment' and
                   e['body']['transaction_id'] == attach['id'] and e['body']['program_cookie'] == cookie]
    host.require(attachments and attachments[0]['state'] == 'attached' and attachments[-1]['state'] == 'released' and
                 attachments[-1]['offline_seq'] is not None and attachments[-1]['release_seq'] is not None and
                 attachments[-1]['offline_seq'] < attachments[-1]['release_seq'] and
                 (item['name'] != 'cgroup-auto-retire' or not any(a['state'] == 'detached' for a in attachments)),
                 'BPF device attachment did not retire with its cgroup')
    group = [e['body'] for e in events if e['kind'] == 'cgroup_enrollment' and
             e['body']['grant_id'] == attach['grant_id'] and e['body']['enrollment'] == attachments[0]['enrollment']]
    brokers = [e['body'] for e in records if e['kind'] == 'principal' and e['body']['role'] == 'BOOTSTRAP_BROKER' and
               e['body']['task_cookie'] == query['executor_cookie']]
    query_events = [e for e in events if e['kind'] == 'transaction' and e['body']['id'] == query['id']]
    attach_events = [e for e in events if e['kind'] == 'attachment' and e['body']['id'] == attachments[0]['id']]
    load_events = [e for e in events if e['kind'] == 'transaction' and e['body']['id'] == load['id'] and
                   e['body']['state'] == 'effect_observed']
    query_audits = [e['body'] for e in events if e['kind'] == 'audit' and e['body']['transaction_id'] == query['id'] and
                    e['body']['record_type'] == 'SYSCALL']
    host.require(len(group) == len(brokers) == len(load_events) == len(query_audits) == 1 and
                 all(query[k] is None for k in ('grant_id', 'invocation_id', 'requester_cookie', 'notification_id', 'returned_fd')) and
                 len(query['input_objects']) == 1 and
                 _shape(query['input_objects'][0], ('field', 'kind', 'cookie', 'requester_fd')) and
                 query['input_objects'][0]['field'] == 'query.target_fd' and query['input_objects'][0]['kind'] == 'cgroup' and
                 query['input_objects'][0]['cookie'] == group[0]['enrollment'] and
                 _integer(query['input_objects'][0]['requester_fd']) and
                 (group[0]['root_cookie'], group[0]['cgroup_id']) == (attachments[0]['root_cookie'], attachments[0]['cgroup_id']) and
                 query_events and query_events[0]['body']['state'] == 'prepared' and
                 load_events[0]['sequence'] < attach_events[0]['sequence'] < query_events[0]['sequence'] < query_events[-1]['sequence'] and
                 query_audits[0]['task_cookie'] == query['executor_cookie'] and query_audits[0]['command'] == 'PROG_QUERY' and
                 query_audits[0]['result'] == query['result'], 'BPF device internal query identity or chronology differs')
    grant_id = attach['grant_id']
    grants = [e['body'] for e in events if e['kind'] == 'grant' and e['body']['id'] == grant_id]
    host.require(grants and grants[-1]['phase'] == 'retired' and any(
        e['kind'] == 'invocation' and e['body']['grant_id'] == grant_id and
        e['body']['command'] == 'start' and e['body']['state'] == 'running' for e in events),
        'BPF device start or grant retirement was not observed')


def _verify_feature_map_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == 'feature-map']
    host.require([c['phase'] for c in cases] == ['started', 'observed', 'settled'] and
                 len({c['nonce'] for c in cases}) == 1 and len(cases[-1]['transaction_ids']) == 1 and
                 len(cases[-1]['reference_cookies']) == 1 and len(cases[-1]['guard_sequences']) == 2 and
                 cases[-1]['actor_cookie'] == item['actor']['task_cookie'] and
                 cases[-1]['result'] == item['syscall_result'], 'BPF feature map index differs')
    txid, cookie = cases[-1]['transaction_ids'][0], cases[-1]['reference_cookies'][0]
    txns = [e['body'] for e in events if e['kind'] == 'transaction' and e['body']['id'] == txid]
    objs = [e['body'] for e in events if e['kind'] == 'object' and e['body']['cookie'] == cookie]
    guards = [e['body']['record'] for e in events if e['kind'] == 'guard' and
              e['body']['record']['sequence'] in cases[-1]['guard_sequences']]
    host.require(txns and txns[-1]['state'] == 'retired' and txns[-1]['command'] == 'MAP_CREATE' and
                 txns[-1]['requester_cookie'] == item['actor']['task_cookie'] and
                 txns[-1]['invocation_id'] == cases[-1]['invocation_id'] and
                 objs and objs[-1]['owner_transaction'] == txid and objs[-1]['class'] == 'map' and
                 [g['kind'] for g in guards] == [3, 4] and all(g['object_cookie'] == cookie for g in guards) and
                 guards[0]['kernel_id'] == guards[1]['kernel_id'] and
                 (objs[-1]['publication_seq'], objs[-1]['removal_seq']) == tuple(cases[-1]['guard_sequences']),
                 'BPF feature map publication or removal differs')
    _runtime_command(host, txns[-1])
    attrs = guards[0]['object_snapshot']['attributes']
    host.require(guards[0]['object_snapshot']['type'] == 2 and
                 (attrs['name'], attrs['key_size'], attrs['value_size'], attrs['max_entries'], attrs['map_flags']) ==
                 ('feature_test', 4, 4, 1, 0), 'BPF feature map snapshot differs')


def _verify_compat_probe_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    cases = [e['body'] for e in events if e['kind'] == 'selftest_case' and e['body']['name'] == item['name']]
    host.require([c['phase'] for c in cases] == ['started', 'observed', 'settled'] and
                 len({c['nonce'] for c in cases}) == 1 and len(cases[-1]['transaction_ids']) == 1 and
                 cases[-1]['actor_cookie'] == item['actor']['task_cookie'] and
                 item['syscall_result'] == cases[-1]['result'] == -9,
                 'BPF compatibility case did not observe EBADF')
    txid = cases[-1]['transaction_ids'][0]
    txns = [e['body'] for e in events if e['kind'] == 'transaction' and e['body']['id'] == txid]
    command = 'PROG_LOAD' if item['name'] == 'extended-info-ebadf' else 'PROG_ATTACH'
    host.require(txns and txns[-1]['state'] == 'retired' and txns[-1]['command'] == command and
                 txns[-1]['result'] == -9 and txns[-1]['returned_fd'] is None and
                 txns[-1]['requester_cookie'] == item['actor']['task_cookie'] and
                 txns[-1]['invocation_id'] == cases[-1]['invocation_id'],
                 'BPF compatibility transaction identity or return differs')
    _runtime_command(host, txns[-1])
    audits = [e['body'] for e in events if e['kind'] == 'audit' and e['body']['record_type'] == 'SYSCALL' and
              e['body']['transaction_id'] == txid]
    host.require(len(audits) == 1 and audits[0]['result'] == -9 and any(
        e['kind'] == 'audit_wire' and e['body']['record_type'] == 1320 and
        e['body']['stamp'] == audits[0]['stamp'] and e['body']['serial'] == audits[0]['serial'] for e in events),
        'BPF compatibility result lacks its syscall audit and EOE')
    if item['name'] == 'extended-info-ebadf':
        host.require(not cases[-1]['reference_cookies'] and not any(
            e['kind'] == 'object' and e['body']['owner_transaction'] == txid for e in events),
            'BPF failed extended-info probe unexpectedly published an object')
    else:
        host.require(len(cases[-1]['reference_cookies']) == 1 and any(
            entry.get('field') == 'attach_bpf_fd' and entry.get('kind') == 'program' and
            entry.get('cookie') == cases[-1]['reference_cookies'][0] for entry in txns[-1]['input_objects']) and
            any(e['kind'] == 'object' and e['body']['cookie'] == cases[-1]['reference_cookies'][0] and
                e['body']['class'] == 'program' for e in events),
            'BPF replacement probe lacks its real program reference')


def _verify_runtime_case(host, item, records):
    events = [records[seq - 1] for seq in item['evidence_sequences']]
    stages = [e['body'] for e in events if e['kind'] == 'selftest_runtime_stage' and e['body']['mode'] == item['name']]
    host.require(stages and stages[0]['phase'] == 'armed' and
                 len({s['nonce'] for s in stages}) == len({s['grant_id'] for s in stages}) == 1,
                 'BPF runtime stage lacks its own arm')
    captured = [s for s in stages if s['phase'] == 'captured']
    host.require(len(captured) == 1 and captured[0]['notifying_cookie'] == item['actor']['task_cookie'] and
                 _integer(captured[0]['invocation_id'], 1) and _integer(captured[0]['notification_id'], 1),
                 'BPF runtime case lacks its captured notifying task')
    invocation_id = captured[0]['invocation_id']
    invocations = [e['body'] for e in events if e['kind'] == 'invocation' and e['body']['id'] == invocation_id]
    txids = {s['transaction_id'] for s in stages if s['transaction_id'] is not None}
    host.require(invocations and len(txids) == 1 and all(i['grant_id'] == stages[0]['grant_id'] for i in invocations),
                 'BPF runtime stage lacks its bound invocation and transaction')
    txid = next(iter(txids))
    txns = [e['body'] for e in events if e['kind'] == 'transaction' and e['body']['id'] == txid]
    host.require(txns and txns[0]['state'] == 'prepared' and all(t['invocation_id'] == invocation_id and
                 t['requester_cookie'] == item['actor']['task_cookie'] and t['command'] == 'PROG_LOAD' for t in txns),
                 'BPF runtime transaction identity differs')
    phases = [s['phase'] for s in stages]
    if item['name'] == 'nested-buffer-race':
        mutations = [s['mutation'] for s in stages if s['mutation'] is not None]
        code = txns[0]['program_bytes']
        host.require('mutated' in phases and 'effect_observed' in phases and 'settled' in phases and
                     type(code) is str and mutations and mutations[0]['captured_sha256'] == hashlib.sha256(bytes.fromhex(code)).hexdigest() and
                     mutations[0]['changed_offset'] == 0 and mutations[0]['mutated_byte'] == 255 and
                     mutations[0]['write_complete'] is True and mutations[-1]['restored'] is True and
                     txns[-1]['state'] == 'retired', 'BPF runtime private-copy race was not settled')
    else:
        cancelled = [s for s in stages if s['phase'] == 'cancelled']
        host.require(len(cancelled) == 1 and _shape(cancelled[0]['signal'], ('task_cookie', 'signal', 'send_result')) and
                     cancelled[0]['signal']['task_cookie'] == invocations[0]['task_cookie'] and
                     cancelled[0]['signal']['signal'] == 9 and cancelled[0]['signal']['send_result'] == 0,
                     'BPF runtime cancellation lacks actual PIDFD signal')
        if item['name'] == 'commit-before-cancel':
            host.require('committed' not in phases and not any(t['state'] in ('committed', 'effect_observed') for t in txns) and
                         any(e['kind'] == 'notification_cancel' and e['body']['notification_id'] == captured[0]['notification_id'] for e in events),
                         'BPF cancellation before commit produced an effect')
        else:
            host.require('effect_observed' in phases and txns[-1]['state'] == 'retired' and
                         txns[-1]['cleanup']['state'] in ('settled', 'not_needed'),
                         'BPF cancellation after effect lacks settled cleanup')
            if item['name'] == 'after-reply-cancel':
                host.require('replied' in phases and phases.index('replied') < phases.index('cancelled') and
                             any(e['kind'] == 'notification_response' and e['body']['transaction_id'] == txid and
                                 e['body']['operation'] == 'ADDFD_SEND' and e['body']['ioctl_errno'] == 0 for e in events),
                             'BPF cancellation did not follow actual descriptor delivery')


def _authorization_observations(host, result, records, hashes, variant='baseline'):
    negative = result.get('negative')
    host.require(_shape(negative, ('version', 'scenario', 'variant', 'observations')) and
                 type(negative['version']) is int and negative['version'] == 1 and negative['variant'] == 'baseline' and
                 negative['scenario'] == 'bpf-authorization' and type(negative['observations']) is list and
                 0 < len(negative['observations']) <= 128, 'BPF authorization observations invalid')
    commands = _COMMAND_IDS
    names, intervals = set(), []
    for item in negative['observations']:
        fields = ('name', 'path', 'actor', 'request', 'syscall_result', 'control_error',
                  'denial_sequence', 'audit', 'before', 'after', 'evidence_sequences')
        if type(item) is dict and item.get('name') == 'extra-fd-delays-removal':
            fields += ('extra_fd',)
        if type(item) is dict and item.get('name') in ('fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse'):
            fields += ('assembly',)
        host.require(_shape(item, fields) and
                     type(item['name']) is str and re.fullmatch(r'[a-z0-9]+(?:-[a-z0-9]+)*', item['name']) is not None and
                     item['name'] not in names and item['path'] in ('raw_syscall', 'user_notification', 'control', 'lifecycle', 'fd_stage'),
                     'BPF authorization observation identity invalid')
        names.add(item['name'])
        actor = item['actor']
        host.require(_shape(actor, ('tgid', 'tid', 'start_ticks', 'task_cookie', 'start_ns')) and
                     all(_integer(actor[k], 1) for k in ('tgid', 'tid', 'start_ticks')) and
                     ((actor['task_cookie'] is None and actor['start_ns'] is None) or
                      (_integer(actor['task_cookie'], 1) and _integer(actor['start_ns'], 1))),
                     'BPF authorization actor invalid')
        request = item['request']
        if request is not None:
            host.require(_shape(request, ('invocation_id', 'notification_id', 'command', 'attributes', 'program_bytes', 'license', 'input_objects')) and
                         all(request[k] is None or _integer(request[k], 1) for k in ('invocation_id', 'notification_id')) and
                         (request['command'] is None or type(request['command']) is str and request['command'] in commands) and
                         (request['license'] is None or type(request['license']) is str and request['license'].isascii() and len(request['license']) <= 63) and
                         (request['program_bytes'] is None or type(request['program_bytes']) is str and
                          re.fullmatch(r'[0-9a-f]*', request['program_bytes']) is not None and len(request['program_bytes']) <= 65536 and
                          len(request['program_bytes']) % 16 == 0) and
                         (request['input_objects'] is None or type(request['input_objects']) is list),
                         'BPF authorization captured request invalid')
            attr = request['attributes']
            host.require(attr is None or _shape(attr, ('attr_size', 'attr_hex')) and _integer(attr['attr_size'], high=4096) and
                         type(attr['attr_hex']) is str and re.fullmatch(r'[0-9a-f]*', attr['attr_hex']) is not None and
                         len(attr['attr_hex']) == 2 * attr['attr_size'], 'BPF authorization captured attributes invalid')
        host.require(item['syscall_result'] is None or _integer(item['syscall_result'], _I64_MIN, _I64_MAX),
                     'BPF authorization syscall result invalid')
        host.require(item['control_error'] is None or item['control_error'] in
                     ('BPF_AUTH_DENIED', 'BPF_AUTH_UNAVAILABLE', 'BPF_AUTH_PENDING', 'BPF_AUTH_INCOMPATIBLE'),
                     'BPF authorization control response invalid')
        if item['audit'] is not None:
            stamp = item['audit']
            host.require(_shape(stamp, ('seconds', 'milliseconds', 'serial')) and _integer(stamp['seconds'], high=_I64_MAX) and
                         _integer(stamp['milliseconds'], high=999) and _integer(stamp['serial'], 1, (1 << 32) - 1),
                         'BPF authorization audit stamp invalid')
        bounds = _authorization_bounds(host, item, records, hashes)
        host.require(item['name'] in _BASELINE_OBSERVATION_NAMES and item['evidence_sequences'] and
                     all(bounds[1] < other[0] or other[1] < bounds[0] for other in intervals),
                     'BPF authorization case borrowed another checkpoint interval')
        intervals.append(bounds)
        case_events = [records[seq - 1]['body'] for seq in item['evidence_sequences'] if
                       records[seq - 1]['kind'] == 'selftest_case' and
                       records[seq - 1]['body']['name'] == item['name']]
        host.require([c['phase'] for c in case_events] == ['started', 'observed', 'settled'] and
                     len({c['nonce'] for c in case_events}) == 1 and
                     case_events[-1]['actor_cookie'] == actor['task_cookie'],
                     'BPF authorization case lacks its own settled actor-bound phases')
        if item['name'] in ('wrong-actor', 'fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse'):
            host.require(all(_hex(c['request_sha256']) and c['nonce'] == c['request_sha256'][:32] for c in case_events) and
                         len({c['request_sha256'] for c in case_events}) == 1,
                         'BPF probe case nonce is not derived from its registration request')
        if item['name'] in ('fd-arm-map', 'fd-arm-program'):
            expected_class = item['name'].removeprefix('fd-arm-')
            referenced = [records[seq - 1] for seq in item['evidence_sequences']]
            stages = [event['body'] for event in referenced if event['kind'] == 'selftest_fd_stage']
            host.require(item['path'] == 'fd_stage' and item['control_error'] is None and
                         item['syscall_result'] in (None, -24) and len(stages) == 3 and
                         [stage['phase'] for stage in stages] == ['armed', 'entered', 'restored'] and
                         all(stage['class'] == expected_class and stage['grant_id'] == stages[0]['grant_id'] and
                             stage['nonce'] == stages[0]['nonce'] for stage in stages),
                         'BPF FD-stage observation lacks its three real stages')
            host.require(stages[1]['original_fd_count'] + stages[1]['identity_fd_count'] + stages[1]['owned_fd_count'] == 256,
                         'BPF successful FD stage did not fill the descriptor table')
            ident = stages[1]['transaction_id']
            transactions = [event['body'] for event in referenced if event['kind'] == 'transaction' and event['body']['id'] == ident]
            objects = [event['body'] for event in referenced if event['kind'] == 'object' and event['body']['owner_transaction'] == ident]
            host.require([txn['state'] for txn in transactions] ==
                         ['prepared', 'committed', 'effect_observed', 'response_settled', 'retired'] and
                         transactions[-1]['result'] == -24 and transactions[-1]['returned_fd'] is None and
                         objects and objects[-1]['class'] == expected_class and objects[-1]['removal_seq'] is not None and
                         any(event['kind'] == 'guard' and event['body']['record']['sequence'] == objects[-1]['publication_seq']
                             for event in referenced) and
                         any(event['kind'] == 'guard' and event['body']['record']['sequence'] == objects[-1]['removal_seq']
                             for event in referenced), 'BPF FD-stage observation lacks real creation and removal')
            audits = [event['body'] for event in referenced if event['kind'] == 'audit' and
                      event['body']['record_type'] == 'SYSCALL' and event['body']['transaction_id'] == ident]
            host.require(len(audits) == 1 and audits[0]['result'] == -24 and
                         any(event['kind'] == 'audit_wire' and event['body']['record_type'] == 1320 and
                             event['body']['stamp'] == audits[0]['stamp'] and event['body']['serial'] == audits[0]['serial']
                             for event in referenced), 'BPF FD-stage observation lacks real syscall and EOE')
        if item['path'] == 'raw_syscall':
            actors = [e['body'] for e in records[bounds[0]:bounds[1] - 1] if e['kind'] == 'probe_actor' and
                      e['body']['name'] == 'raw-denial' and e['body']['tgid'] == actor['tgid'] and
                      e['body']['tid'] == actor['tid'] and e['body']['start_ns'] // 10000000 == actor['start_ticks']]
            host.require(item['name'] == 'wrong-actor' and len(actors) == 1 and
                         (actor['task_cookie'] is None or actor['task_cookie'] == actors[0]['task_cookie']) and
                         (actor['start_ns'] is None or actor['start_ns'] == actors[0]['start_ns']) and
                         item['syscall_result'] == -1 and item['control_error'] is None,
                         'BPF raw denial lacks its registered probe actor')
            raw = [e['body']['record'] for e in records[bounds[0]:bounds[1] - 1] if e['kind'] == 'guard' and
                   e['body']['record']['kind'] == 13 and e['body']['record']['task_cookie'] == actors[0]['task_cookie']]
            host.require([(g['operation'], g['result']) for g in raw] == [(0, -1), (5, -1)] and
                         all(item['before']['guard_sequence'] < g['sequence'] <= item['after']['guard_sequence'] for g in raw) and
                         not any(e['kind'] == 'guard' and e['body']['record']['kind'] in (1, 2, 3, 4, 5, 6)
                                 for e in records[bounds[0]:bounds[1] - 1]),
                         'BPF raw denial lacks its actual ordered calls or no-effect interval')
            _replay_raw_evidence(host, records[bounds[0]:bounds[1] - 1], records)
        elif item['path'] in ('control', 'lifecycle'):
            if item['name'] == 'id-reuse' and item['path'] == 'lifecycle':
                _verify_id_reuse(host, item, records)
            elif item['name'] in ('fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse') and item['path'] == 'lifecycle':
                _verify_assembly_case(host, item, records)
            elif item['name'] in ('nested-buffer-race', 'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel') and item['path'] == 'lifecycle':
                _verify_runtime_case(host, item, records)
            elif item['name'] == 'feature-map' and item['path'] == 'lifecycle':
                _verify_feature_map_case(host, item, records)
            elif item['name'] in ('extended-info-ebadf', 'replace-probe-ebadf') and item['path'] == 'lifecycle':
                _verify_compat_probe_case(host, item, records)
            elif item['name'] in ('allowed-device-start', 'cgroup-auto-retire') and item['path'] == 'lifecycle':
                _verify_device_lifecycle_case(host, item, records)
            elif item['name'] in ('attachment-replace', 'failure-cleanup') and item['path'] == 'lifecycle':
                _verify_program_cleanup_case(host, item, records)
            elif item['name'] == 'extra-fd-delays-removal' and item['path'] == 'lifecycle':
                _verify_extra_fd_case(host, item, records)
            elif item['name'] == 'audit-interleaved' and item['path'] == 'lifecycle':
                _verify_interleaved_case(host, item, records)
            elif item['name'] == 'container-bpf' and item['path'] == 'lifecycle':
                _verify_container_probe_case(host, item, records)
            elif item['name'] == 'seal-detach-denied' and item['path'] == 'lifecycle':
                _verify_seal_detach_case(host, item, records, result['bpf_bootstrap'])
            elif item['name'] == 'seal-unload-denied' and item['path'] == 'lifecycle':
                _verify_seal_unload_case(host, item, records, result['bpf_bootstrap'])
            elif item['path'] == 'control':
                _verify_control_case(host, item, records)
            else:
                host.require(False, 'BPF authorization %s lacks a replayable typed proof' % item['name'])
        if item['path'] == 'user_notification':
            seq = item['denial_sequence']
            host.require(_integer(seq, bounds[0] + 1, bounds[1] - 1) and seq in item['evidence_sequences'] and
                         records[seq - 1]['kind'] == 'notification_denial' and request is not None and
                         request['command'] in commands and item['syscall_result'] == -1 and item['audit'] is None and
                         item['control_error'] is None, 'BPF notification denial evidence invalid')
            denial = records[seq - 1]['body']
            host.require(denial['invocation_id'] == request['invocation_id'] and denial['notification_id'] == request['notification_id'] and
                         denial['requester_cookie'] == actor['task_cookie'] and denial['command'] == commands[request['command']] and
                         item['before']['guard_sequence'] <= denial['guard_before'] <= denial['guard_after'] <= item['after']['guard_sequence'],
                         'BPF denial differs from actual invocation or guard bounds')
            identity = (actor['tgid'], actor['tid'], actor['start_ns'])
            observed = []
            for event in records[:seq]:
                if event['kind'] == 'principal' and event['body']['task_cookie'] == actor['task_cookie']:
                    principal = event['body']
                    observed.append((principal['tgid'], principal['tid'], principal['start_ns']))
                elif event['kind'] == 'guard' and event['body']['record']['kind'] in (9, 10, 11, 12):
                    guard = event['body']['record']
                    if guard['task_cookie'] == actor['task_cookie']:
                        observed.append((guard['detail']['tgid'], guard['detail']['tid'], guard['task_start_ns']))
            host.require(observed and all(value == identity for value in observed),
                         'BPF denied notification actor incarnation is not independently observed')
            interval = records[bounds[0]:bounds[1] - 1]
            host.require(not any(event['kind'] == 'transaction' and
                                 event['body']['invocation_id'] == denial['invocation_id'] and
                                 event['body']['notification_id'] == denial['notification_id'] or
                                 event['kind'] == 'guard' and event['body']['record']['kind'] in (1, 2, 3, 4, 5, 6) and
                                 denial['guard_before'] < event['body']['record']['sequence'] <= denial['guard_after']
                                 for event in interval), 'BPF denied notification produced an effect')
            host.require(item['name'] in ('wrong-program-byte', 'wrong-flags', 'wrong-target'),
                         'BPF unknown notification denial case')
            stages = [e['body'] for e in interval if e['kind'] == 'selftest_runtime_stage' and
                      e['body']['mode'] == item['name'] and e['body']['mutation'] is not None and
                      e['body']['invocation_id'] == denial['invocation_id'] and
                      e['body']['notification_id'] == denial['notification_id'] and
                      e['body']['notifying_cookie'] == actor['task_cookie']]
            host.require(len(stages) == 1, 'BPF denial lacks its own captured mutation')
            mutation = stages[0]['mutation']
            fields = ('kind', 'before_sha256', 'after_sha256', 'attr_size', 'attr_hex', 'program_bytes', 'license', 'target_cookie')
            if item['name'] == 'wrong-target':
                fields += ('target_identity',)
            host.require(_shape(mutation, fields) and
                         mutation['kind'] == item['name'] and _hex(mutation['before_sha256']) and
                         _hex(mutation['after_sha256']) and mutation['before_sha256'] != mutation['after_sha256'] and
                         request['attributes'] == {'attr_size': mutation['attr_size'], 'attr_hex': mutation['attr_hex']} and
                         request['program_bytes'] == mutation['program_bytes'] and request['license'] == mutation['license'],
                         'BPF denied request differs from captured mutation')
            if item['name'] == 'wrong-program-byte':
                expected_program = bytearray(_device_program())
                host.require(request['command'] == 'PROG_LOAD' and type(mutation['program_bytes']) is str and
                             mutation['before_sha256'] == hashlib.sha256(expected_program).hexdigest(),
                             'BPF wrong-program original input differs')
                expected_program[0] = 255
                host.require(bytes.fromhex(mutation['program_bytes']) == expected_program and
                             hashlib.sha256(expected_program).hexdigest() == mutation['after_sha256'],
                             'BPF wrong-program case did not capture the changed opcode')
            elif item['name'] == 'wrong-flags':
                attrs = bytes.fromhex(mutation['attr_hex'])
                original_attrs = bytearray(attrs)
                if len(original_attrs) >= 16:
                    struct.pack_into('<I', original_attrs, 12, 2)
                host.require(request['command'] == 'PROG_ATTACH' and len(attrs) >= 16 and
                             struct.unpack_from('<I', attrs, 12)[0] == 1 << 31 and
                             hashlib.sha256(original_attrs).hexdigest() == mutation['before_sha256'] and
                             hashlib.sha256(attrs).hexdigest() == mutation['after_sha256'],
                             'BPF wrong-flags case did not capture disallowed flags')
            else:
                target = mutation['target_identity']
                attrs = bytes.fromhex(mutation['attr_hex'])
                invocations = [e['body'] for e in interval if e['kind'] == 'invocation' and
                               e['body']['id'] == denial['invocation_id']]
                host.require(request['command'] == 'PROG_ATTACH' and mutation['target_cookie'] is None and
                             _shape(target, ('original_device', 'original_inode', 'substitute_device', 'substitute_inode')) and
                             all(_integer(target[k], 1) for k in target) and
                             target['original_device'] == target['substitute_device'] and
                             target['original_inode'] != target['substitute_inode'] and len(attrs) >= 16 and
                             hashlib.sha256(attrs).hexdigest() == mutation['after_sha256'] and
                             invocations and all(inv['grant_id'] == stages[0]['grant_id'] for inv in invocations),
                             'BPF wrong-target case lacks the distinct imported cgroup identity')
                grant_id = stages[0]['grant_id']
                host.require(not any(e['kind'] == 'attachment' and e['body']['grant_id'] == grant_id and
                                     e['body']['state'] == 'attached' for e in interval),
                             'BPF wrong-target create attached a program')
        else:
            host.require(item['denial_sequence'] is None, 'BPF non-notification observation names a denial')
    host.require(names == _BASELINE_OBSERVATION_NAMES, 'BPF authorization baseline negative inventory is incomplete or unknown')


def _replay_healthy_partial(host, result, prefix, build, account, policy_elf, target_btf, scenario):
    records, hashes = _partial_chain(host, prefix, result.get('bpf_healthy_partial'))
    _replay_runtime_root(host, records, prefix, build)
    _replay_module_instance(host, records, prefix)
    _bootstrap_policy(host, records[:len(prefix['records'])], prefix, policy_elf, target_btf)
    _replay_programs(host, records)
    _replay_ancestry(host, records, prefix, build)
    _replay_exec_lifecycle(host, records, complete=False)
    _replay_transition_sources(host, records, result)
    _replay_raw_evidence(host, records, records)
    checkpoints = [e['sequence'] for e in records if e['kind'] == 'checkpoint' and e['body']['state'] == 'released']
    host.require(checkpoints, 'BPF terminal case lacks an actual released healthy checkpoint')
    end = checkpoints[-1]
    browser_image = build['browser_selection']['selection']['image_id']
    host.require(type(browser_image) is str and browser_image.startswith('sha256:') and _hex(browser_image[7:]),
                 'BPF browser image pin invalid')
    healthy = records[:end]
    _replay_continuity(host, healthy, prefix, hashes[:end + 1], account, browser_image, scenario)
    _replay_audit(host, healthy)
    _replay_readbacks(host, healthy, hashes[:end + 1], prefix)
    host.require(all(e['kind'] in ('probe_actor', 'principal') or e['kind'] == 'guard' and
                     e['body']['record']['kind'] in (9, 10, 11, 12) for e in records[end:]),
                 'BPF post-checkpoint partial prefix has unverified effects')
    return records


def _fault_pages(host, frames):
    pages = {}
    total = 0
    for frame in frames:
        if frame['kind'] not in ('reader_witness_chunk', 'broker_witness_chunk'):
            continue
        role = frame['role']
        host.require(role in ('original', 'modified', 'original_reply', 'sent_reply') and
                     _integer(frame['total_bytes'], 1, 32768) and
                     _integer(frame['page_count'], 1, 4) and
                     frame['page_count'] == (frame['total_bytes'] + 8191) // 8192 and
                     _integer(frame['page_index'], 0, frame['page_count'] - 1) and
                     _hex(frame['total_sha256']) and type(frame['data_b64']) is str,
                     'BPF fault witness page metadata invalid')
        try:
            data = base64.b64decode(frame['data_b64'], validate=True)
        except (ValueError, binascii.Error) as exc:
            raise host.Failure('BPF fault witness page encoding invalid') from exc
        host.require(base64.b64encode(data).decode('ascii') == frame['data_b64'] and
                     0 < len(data) <= 8192 and
                     (frame['page_index'] + 1 == frame['page_count'] or len(data) == 8192),
                     'BPF fault witness page size or encoding invalid')
        page = pages.setdefault(role, {'data': bytearray(), 'next': 0, 'count': frame['page_count'],
                                       'size': frame['total_bytes'], 'sha': frame['total_sha256']})
        host.require(frame['page_index'] == page['next'] and frame['page_count'] == page['count'] and
                     frame['total_bytes'] == page['size'] and frame['total_sha256'] == page['sha'],
                     'BPF fault witness pages are missing or inconsistent')
        page['data'].extend(data)
        page['next'] += 1
        total += len(data)
        host.require(total <= 32768, 'BPF fault witness pages exceed their bound')
    for page in pages.values():
        host.require(page['next'] == page['count'] and len(page['data']) == page['size'] and
                     hashlib.sha256(page['data']).hexdigest() == page['sha'],
                     'BPF fault witness page digest differs')
    return {role: bytes(page['data']) for role, page in pages.items()}


def _verify_fault_observation(host, result, prefix, records, variant):
    negative = result.get('negative')
    host.require(_shape(negative, ('version', 'scenario', 'variant', 'terminal_observation')) and
                 type(negative['version']) is int and negative['version'] == 1 and
                 negative['scenario'] == 'bpf-authorization' and negative['variant'] == variant,
                 'BPF fault observation identity differs')
    term = negative['terminal_observation']
    _verify_terminal_observation_obj(host, term, variant, 'closure_on_fault', prefix=prefix, actor_records=records)
    before, after = term['authority_before'], term['authority_after']
    observed = [e['body']['record'] for e in term['records'][term['begin_sequence']:] if e['kind'] == 'guard']
    host.require(term['broker_exit'] is not None and term['broker_exit']['task_cookie'] == before['broker_cookie'] and
                 term['broker_exit']['pidfd_exited'] is True and after['authority_phase'] == 0 and after['state'] == 'dead' and
                 any(g['kind'] == 14 and g['operation'] == 2 and g['task_cookie'] == before['broker_cookie'] for g in observed),
                 'BPF fault lacks observed cleanup-only transition and broker closure')
    detectors = {
        'snapshot-missing': ('broker_guard_copy', 'guard_snapshot_missing'),
        'snapshot-bad-field': ('broker_guard_copy', 'guard_snapshot_mismatch'),
        'audit-loss': ('broker_audit_delivery', 'audit_incomplete'),
        'audit-duplicate': ('broker_audit_delivery', 'audit_duplicate'),
        'audit-overflow': ('broker_audit_delivery', 'audit_wire_capacity'),
        'reader-death': ('broker_reader', 'reader_pidfd_exited'),
        'reader-endpoint': ('broker_reader', 'reader_endpoint_closed'),
        'reader-timeout': ('broker_reader', 'reader_deadline'),
        'reader-replay': ('broker_reader', 'reader_response_identity'),
        'reader-credential': ('broker_reader', 'reader_message_credentials'),
        'reader-extra-fd': ('broker_reader', 'reader_unexpected_rights'),
        'reader-frame': ('broker_reader', 'reader_noncanonical_frame'),
        'reader-done': ('broker_reader', 'reader_missing_done'),
        'fd-fill-failure': ('broker_fd_stage', 'fd_fill_failure'),
        'fd-restore-failure': ('broker_fd_stage', 'fd_restore_failure'),
    }
    if variant in detectors:
        proof = term['fault_evidence']
        host.require(_shape(proof, 'variant source detector request_id nonce input_sha256 input_bytes expected_actor observed_actor deadline_ns observed_ns capacity count_before rejected_wire guard_sequences reader_witness_sequences broker_witness_sequences'.split()) and
                     proof['variant'] == variant and (proof['source'], proof['detector']) == detectors[variant],
                     'BPF fault detector evidence differs')
        for key, kind in (('reader_witness_sequences', 'reader_witness'), ('broker_witness_sequences', 'broker_witness')):
            seqs = proof[key]
            host.require(type(seqs) is list and seqs == sorted(set(seqs)) and
                         all(_integer(seq, term['begin_sequence'] + 1, term['end_sequence']) and
                             term['records'][seq - 1]['kind'] == kind for seq in seqs),
                         'BPF fault witness references differ')
        witnesses = [term['records'][seq - 1]['body'] for seq in proof['broker_witness_sequences']]
        readers = [term['records'][seq - 1]['body'] for seq in proof['reader_witness_sequences']]
        for kind, seqs in (('broker_witness', proof['broker_witness_sequences']),
                           ('reader_witness', proof['reader_witness_sequences'])):
            host.require(seqs == [e['sequence'] for e in term['records'][term['begin_sequence']:] if e['kind'] == kind],
                         'BPF fault witness list omits an observed frame')
        receives = [w for w in witnesses if w['kind'] == 'broker_receive_observation']
        host.require(len(receives) == 1 and all(receives[0][key] == proof[key] for key in
                     ('variant', 'source', 'detector', 'expected_actor', 'observed_actor', 'deadline_ns',
                      'observed_ns', 'capacity', 'count_before', 'rejected_wire')),
                     'BPF detector does not join its broker witness')
        witness = receives[0]
        pages = _fault_pages(host, witnesses + readers)
        host.require(type(proof['guard_sequences']) is list and proof['guard_sequences'] == sorted(set(proof['guard_sequences'])) and
                     all(_integer(seq, term['begin_sequence'] + 1, term['end_sequence']) and
                         term['records'][seq - 1]['kind'] == 'guard' for seq in proof['guard_sequences']),
                     'BPF fault guard references differ')
        if proof['source'] == 'broker_guard_copy':
            original, modified = pages.get('original'), pages.get('modified')
            host.require(original is not None and modified is not None and len(original) == len(modified) == 192 and
                         witness['original_sha256'] == hashlib.sha256(original).hexdigest() and
                         proof['input_sha256'] == witness['input_sha256'] == hashlib.sha256(modified).hexdigest() and
                         proof['input_bytes'] == witness['input_bytes'] == 192,
                         'BPF snapshot copies or their digests differ')
            host.require(struct.unpack_from('<I', original, 128)[0] == 2 and
                         struct.unpack_from('<I', original, 156)[0] == 4,
                         'BPF original publication is not the expected array snapshot')
            expected = bytearray(original)
            if variant == 'snapshot-missing':
                expected[128:192] = bytes(64)
            else:
                host.require(struct.unpack_from('<I', original, 156)[0] == 4,
                             'BPF original snapshot value size differs')
                struct.pack_into('<I', expected, 156, 8)
            publications = [term['records'][seq - 1]['body']['record'] for seq in proof['guard_sequences'] if
                            term['records'][seq - 1]['body']['record']['kind'] == 3 and
                            term['records'][seq - 1]['body']['record']['sequence'] == witness['guard_sequence']]
            host.require(modified == expected and len(publications) == 1 and
                         struct.unpack_from('<Q', original, 0)[0] == publications[0]['sequence'] and
                         struct.unpack_from('<I', original, 8)[0] == 3 and
                         struct.unpack_from('<Q', original, 16)[0] == publications[0]['object_cookie'] and
                         struct.unpack_from('<Q', original, 24)[0] == publications[0]['kernel_id'] and
                         publications[0]['object_snapshot']['type'] == 2 and
                         publications[0]['object_snapshot']['attributes']['value_size'] == 4,
                         'BPF snapshot mutation or native publication differs')
        elif proof['source'] == 'broker_audit_delivery':
            wire = proof['rejected_wire']
            host.require(_shape(wire, ('stamp', 'serial', 'record_type', 'payload_sha256', 'occurrence')) and
                         _hex(wire['payload_sha256']) and proof['input_sha256'] == witness['input_sha256'] == wire['payload_sha256'],
                         'BPF rejected audit wire invalid')
            matches = [e['body'] for e in term['records'] if e['kind'] == 'audit_wire' and
                       all(e['body'][k] == wire[k] for k in ('stamp', 'serial', 'record_type', 'payload_sha256'))]
            host.require(len(matches) == 1 and matches[0]['occurrence'] == 1 and not term['losses']['audit'],
                         'BPF rejected wire lacks its independent observer copy')
            if variant == 'audit-overflow':
                host.require(proof['capacity'] == proof['count_before'] == _MAX_AUDIT_WIRE_IDENTITIES and wire['occurrence'] == 1,
                             'BPF audit capacity boundary differs')
            else:
                host.require((wire['record_type'], wire['occurrence']) ==
                             ((1320, 1) if variant == 'audit-loss' else (1300, 2)) and
                             _integer(proof['expected_actor'], 1) and any(
                                 e['kind'] == 'audit' and e['body']['stamp'] == wire['stamp'] and
                                 e['body']['serial'] == wire['serial'] and e['body']['record_type'] == 'SYSCALL' and
                                 e['body']['result'] == -1 for e in term['records']),
                             'BPF audit delivery fault lacks its denied syscall and EOE')
        elif proof['source'] == 'broker_fd_stage':
            host.require(proof['capacity'] == 256 and _integer(proof['input_bytes'], 256) and
                         proof['input_bytes'] == witness['input_bytes'] and
                         _integer(proof['count_before'], 0, 256) and
                         (proof['count_before'] < 256 if variant == 'fd-fill-failure' else proof['count_before'] == 256),
                         'BPF FD-stage fault does not prove the attempted fill or restore')
        else:
            host.require(readers and _integer(proof['expected_actor'], 1), 'BPF reader witness missing')
            reader_principals = [e['body'] for e in records if e['kind'] == 'principal' and
                                 e['body']['role'] == 'BOOTSTRAP_AUDIT_READER']
            host.require(len(reader_principals) == 1 and reader_principals[0]['tgid'] == proof['expected_actor'] and
                         len([r for r in readers if r['kind'] == 'reader_witness_arm']) == 1,
                         'BPF reader witness is not bound to the original reader')
            if variant == 'reader-death':
                exits = [r for r in readers if r['kind'] == 'reader_pidfd_exit']
                host.require(len(exits) == 1 and exits[0]['reader_pid'] == proof['expected_actor'] and
                             exits[0]['pidfd_exited'] is True and exits[0]['query_dup_closed_after_exit'] is True,
                             'BPF reader PIDFD exit and held sender closure were not independently observed')
            events = [r for r in readers if r['kind'] == 'reader_witness_event']
            if variant != 'reader-death':
                host.require(len(events) == 1 and events[0]['request_id'] == proof['request_id'] == witness['request_id'] and
                             events[0]['nonce'] == proof['nonce'] == witness['nonce'],
                             'BPF reader request identity differs')
                event = events[0]
                if variant == 'reader-endpoint':
                    host.require(event['endpoint_closed'] is True and event['send_result'] is None,
                                 'BPF reader endpoint was not closed')
                elif variant == 'reader-timeout':
                    host.require(event['check'] == 'status' and event['send_result'] is None and
                                 _integer(proof['deadline_ns'], 1) and _integer(proof['observed_ns'], proof['deadline_ns']),
                                 'BPF reader deadline was not reached')
                else:
                    recv = witness['receive_observation']
                    host.require(type(recv) is dict and _hex(proof['input_sha256']) and
                                 proof['input_sha256'] == witness['input_sha256'] == event['sent_reply_sha256'] == recv.get('packet_sha256') and
                                 proof['input_bytes'] == witness['input_bytes'] == event['sent_reply_bytes'] == recv.get('bytes') and
                                 recv.get('msg_trunc') is False and recv.get('msg_ctrunc') is False and
                                 recv.get('uid') == recv.get('gid') == 0 and recv.get('pid') == proof['observed_actor'],
                                 'BPF reader receive does not match the witnessed input')
                    if variant == 'reader-credential':
                        host.require(proof['observed_actor'] != proof['expected_actor'] and
                                     proof['observed_actor'] == term['broker_exit']['tgid'] and
                                     event['send_result'] is None and pages.get('original_reply') is not None and
                                     pages['original_reply'] == pages.get('sent_reply') and
                                     any(w['kind'] == 'broker_credential_send' and w['input_sha256'] == proof['input_sha256'] for w in witnesses),
                                     'BPF credential fault lacks broker-originated packet')
                    else:
                        host.require(proof['observed_actor'] == proof['expected_actor'] and event['send_result'] == 0,
                                     'BPF reader packet sender differs')
                        if variant == 'reader-extra-fd':
                            host.require(event['sent_rights'] == recv.get('rights_count') == recv.get('closed_count') == 1,
                                         'BPF unexpected descriptor was not received and closed')
                        else:
                            original, sent = pages.get('original_reply'), pages.get('sent_reply')
                            host.require(original is not None and sent is not None and
                                         hashlib.sha256(sent).hexdigest() == proof['input_sha256'] and
                                         hashlib.sha256(original).hexdigest() == event['original_reply_sha256'] and
                                         recv.get('rights_count') == 0, 'BPF reader packet copies differ')
                            expected_reply = _canonical({'version': 1, 'boot_id': term['boot_id'],
                                'request_id': event['request_id'], 'nonce': event['nonce'],
                                'check': event['check'], 'queries': event['queries']})
                            host.require(original == expected_reply, 'BPF original reader reply differs from its queries')
                            if variant == 'reader-frame':
                                host.require(sent == original + b' ', 'BPF malformed reader frame differs')
                            elif variant == 'reader-replay':
                                try:
                                    old = json.loads(sent)
                                except (ValueError, UnicodeError) as exc:
                                    raise host.Failure('BPF replay frame invalid') from exc
                                host.require(_canonical(old) == sent and old.get('boot_id') == term['boot_id'] and
                                             old.get('request_id') == proof['request_id'] - 1 and old.get('nonce') != proof['nonce'],
                                             'BPF replay is not the prior reader response')
                            else:
                                queries = event['queries']
                                host.require(event['check'] == 'configuration' and type(queries) is list and len(queries) == 3,
                                             'BPF reader DONE original queries missing')
                                _audit_queries(host, {'check': 'configuration', 'queries': queries}, 0, None)
                                rows = queries[1]['replies']
                                host.require(rows and rows[-1]['type'] == 3, 'BPF original LIST has no DONE')
                                modified_queries = [dict(q) for q in queries]
                                modified_queries[1]['replies'] = rows[:-1]
                                modified_reply = {'version': 1, 'boot_id': term['boot_id'], 'request_id': event['request_id'],
                                                  'nonce': event['nonce'], 'check': 'configuration', 'queries': modified_queries}
                                host.require(sent == _canonical(modified_reply), 'BPF reader DONE omission differs')
        return
    if variant == 'guard-overflow':
        host.require(after['sticky'] != 0 and after['lost'] > before['lost'] and
                     after['sequence'] - before['sequence'] >= 4096,
                     'BPF native overflow lacks actual ring saturation and loss')
    elif variant == 'guard-miss':
        host.require(after['sticky'] != 0 and after['probe_missed'] > before['probe_missed'],
                     'BPF native probe miss lacks an observed missed probe')
    elif variant == 'guard-pairing':
        enters = [g for g in observed if g['kind'] == 7]
        leaves = [g for g in observed if g['kind'] == 8]
        host.require(after['sticky'] != 0 and after['lost'] > before['lost'] and
                     any(not any(all(g[k] == h[k] for k in ('task_cookie', 'task_start_ns', 'object_cookie', 'kernel_id', 'operation'))
                                 and h['sequence'] > g['sequence'] for h in leaves) for g in enters),
                     'BPF pairing fault lacks the actual unmatched native pair and loss')
    else:
        host.require(False, 'BPF %s lacks typed evidence of the actual declared fault' % variant)


def _broker_kill_phases(host, term, broker):
    after = term['authority_after']
    interval = [e['body']['record'] for e in term['records'][term['begin_sequence']:] if e['kind'] == 'guard']
    phases = [g for g in interval if g['kind'] == 14]
    exits = [g for g in interval if g['kind'] == 15]
    host.require([g['operation'] for g in phases] == [2, 0] and len(exits) == 1 and
                 all(g['task_cookie'] == broker['task_cookie'] and g['task_start_ns'] == broker['start_ns'] for g in phases) and
                 phases[0]['sequence'] < phases[1]['sequence'] < exits[0]['sequence'] == after['death_sequence'],
                 'BPF broker phase transitions or death ordering differ')
    calls = [g for g in interval if g['kind'] == 13 and g['task_cookie'] == broker['task_cookie']]
    phase2 = [g for g in calls if phases[0]['sequence'] < g['sequence'] < phases[1]['sequence']]
    phase0 = [g for g in calls if phases[1]['sequence'] < g['sequence'] < exits[0]['sequence']]
    host.require([(g['operation'], g['result']) for g in phase2] == [(0, -1), (5, -1), (8, -1), (1, 0)] and
                 [(g['operation'], g['result']) for g in phase0] == [(0, -1), (5, -1), (8, -1), (1, -1)] and
                 len(calls) == 8 and not any(g['kind'] in (1, 2, 3, 4, 5, 6) for g in interval
                                            if phases[0]['sequence'] < g['sequence']),
                 'BPF broker live phase denials, cleanup lookup, or no-effect interval differs')
    return interval, exits[0]


def verify_broker_kill(host, result, record, build):
    prefix = result.get('bpf_bootstrap')
    _build_binding(host, prefix if type(prefix) is dict else {}, build)
    import source_boot
    try:
        _selected_observer(host, build)
        account = _postgres_account(host, build)
    except source_boot.PrerequisiteError as exc:
        raise host.Failure('BPF observer source verification failed') from exc
    target_btf, policy_elf = _kernel_btf(host, build), _policy_elf(host, build)
    records = _replay_healthy_partial(host, result, prefix, build, account, policy_elf, target_btf, 'bpf-broker-kill')
    neg = result.get('negative')
    host.require(_shape(neg, ('version', 'scenario', 'terminal_observation', 'post_death_probe')) and
                 type(neg['version']) is int and neg['version'] == 1 and neg['scenario'] == 'bpf-broker-kill',
                 'BPF broker-kill negative evidence incomplete')
    term = neg['terminal_observation']
    _verify_terminal_observation_obj(host, term, 'bpf-broker-kill', 'denial_proved', prefix=prefix, actor_records=records)
    brokers = [e['body'] for e in records if e['kind'] == 'principal' and e['body']['id'] == prefix['broker_principal']]
    host.require(len(brokers) == 1, 'BPF original broker principal missing')
    broker = brokers[0]
    before, after = term['authority_before'], term['authority_after']
    exit_proof = term['broker_exit']
    host.require(after['state'] == 'dead' and after['authority_phase'] == 0 and
                 after['death_sequence'] == exit_proof['guard_exit_sequence'] and exit_proof['guard_exit_code'] == 9 and
                 exit_proof['direct_wait'] is None and exit_proof['task_cookie'] == before['broker_cookie'] == broker['task_cookie'] and
                 all(exit_proof[k] == broker[k] for k in ('tgid', 'tid', 'start_ns')),
                 'BPF original broker lacks its actual SIGKILL and pidfd exit')
    interval, broker_exit = _broker_kill_phases(host, term, broker)
    probes = [e['body'] for e in records if e['kind'] == 'probe_actor' and e['body']['name'] == 'post-death-raw']
    host.require(len(probes) == 1 and len(term['probe_exits']) == 1, 'BPF post-death probe registration or exit missing')
    probe, probe_exit = probes[0], term['probe_exits'][0]
    host.require(probe_exit['task_cookie'] == probe['task_cookie'] and
                 all(probe_exit[k] == probe[k] for k in ('tgid', 'tid', 'start_ns')) and
                 probe_exit['direct_wait'] is not None and probe_exit['direct_wait']['waiter_cookie'] == probe['parent_cookie'] and
                 probe_exit['guard_exit_code'] == 0 and probe_exit['guard_exit_sequence'] > broker_exit['sequence'],
                 'BPF post-death probe exit is not the registered child')
    raw = [g for g in interval if g['kind'] == 13 and g['task_cookie'] == probe['task_cookie']]
    host.require([(g['operation'], g['result']) for g in raw] == [(0, -1), (5, -1)] and
                 all(broker_exit['sequence'] < g['sequence'] < probe_exit['guard_exit_sequence'] for g in raw),
                 'BPF post-death probe did not execute its actual two denials')
    report = neg['post_death_probe']
    host.require(_shape(report, ('version', 'case', 'actor', 'attempts')) and report['version'] == 1 and
                 report['case'] == 'bpf-authorization' and _shape(report['actor'], ('tgid', 'tid', 'start_ticks', 'parent_tgid')) and
                 report['actor']['tgid'] == probe['tgid'] and report['actor']['tid'] == probe['tid'] and
                 report['actor']['start_ticks'] == probe['start_ns'] // 10000000 and type(report['attempts']) is list and
                 len(report['attempts']) == 2 and all(type(a) is dict for a in report['attempts']) and
                 [(a.get('command'), a.get('result')) for a in report['attempts']] == [('MAP_CREATE', -1), ('PROG_LOAD', -1)],
                 'BPF post-death report differs from the watched probe')

def verify(host, result, record, build, *, reboot_result=None):
    """Check the original boot and, for vm-kill, the distinct reboot chain."""
    try:
        host.require(type(result) is dict and type(record) is dict and
                     result.get('case') == record.get('case') and result.get('scenario') == record.get('scenario'),
                     'BPF result and receipt identity differs')
        host.require(len(_canonical(result)) <= _MAX_RESULT_BYTES, 'BPF containing result exceeds limit')
        case, scenario = record['case'], record['scenario']
        host.require(type(reboot_result) is dict if (case, scenario) == ('source-recovery', 'vm-kill')
                     else reboot_result is None, 'BPF reboot result is invalid for case')
        variant = record.get('bpf_variant') or 'baseline'
        host.require(scenario not in ('bpf-authorization', 'bpf-broker-kill') or case == 'realm-negative',
                     'BPF standalone scenario is outside its authorized case')
        if scenario == 'bpf-broker-kill':
            verify_broker_kill(host, result, record, build)
            return
        if scenario == 'bpf-authorization' and (variant in _TERMINAL_FAULT_VARIANTS or variant in _READER_FAULT_VARIANTS):
            prefix = result.get('bpf_bootstrap')
            _build_binding(host, prefix if type(prefix) is dict else {}, build)
            import source_boot
            try:
                _selected_observer(host, build)
                account = _postgres_account(host, build)
            except source_boot.PrerequisiteError as exc:
                raise host.Failure('BPF observer source verification failed') from exc
            target_btf, policy_elf = _kernel_btf(host, build), _policy_elf(host, build)
            partial = _replay_healthy_partial(host, result, prefix, build, account, policy_elf, target_btf, 'bpf-broker-kill')
            _verify_fault_observation(host, result, prefix, partial, variant)
            return
        genesis = result.get('genesis')
        if type(genesis) is dict:
            prefix = genesis.get('bpf_authority')
            host.require(prefix is not None, 'BPF realm genesis authority missing')
        else:
            host.require(case == 'realm-negative' and scenario in _STANDALONE,
                         'BPF standalone bootstrap is not authorized for this case')
            prefix = result.get('bpf_bootstrap')
        suffix = result.get('bpf_authority')
        _build_binding(host, prefix if type(prefix) is dict else {}, build)
        import source_boot
        try:
            _selected_observer(host, build)
            account = _postgres_account(host, build)
        except source_boot.PrerequisiteError as exc:
            raise host.Failure('BPF observer source verification failed') from exc
        target_btf = _kernel_btf(host, build)
        policy_elf = _policy_elf(host, build)
        records, hashes = _chain(host, prefix, suffix)
        _replay_runtime_root(host, records, prefix, build)
        _replay_module_instance(host, records, prefix)
        _replay_raw_evidence(host, records, records)
        _replay_exec_lifecycle(host, records)
        _replay_transition_sources(host, records, result)
        if scenario == 'bpf-authorization':
            _authorization_observations(host, result, records, hashes, variant=variant)
        _replay_programs(host, records)
        _replay_ancestry(host, records, prefix, build)
        browser_image = build['browser_selection']['selection']['image_id']
        host.require(type(browser_image) is str and browser_image.startswith('sha256:') and
                     _hex(browser_image[7:]), 'BPF browser image pin invalid')
        _replay_continuity(host, records, prefix, hashes, account, browser_image, scenario)
        _replay_audit(host, records)
        _replay_readbacks(host, records, hashes, prefix)
        _bootstrap_policy(host, records, prefix, policy_elf, target_btf)
        if type(genesis) is dict:
            host.require(genesis.get('boot_id') == prefix['boot_id'], 'BPF authority differs from realm boot')
        if case == 'source-recovery' and scenario == 'vm-kill':
            reboot = reboot_result
            host.require(type(reboot) is dict and reboot.get('case') == case and reboot.get('scenario') == scenario,
                         'BPF reboot result missing')
            new_prefix, new_suffix = reboot.get('bpf_bootstrap'), reboot.get('bpf_authority')
            _build_binding(host, new_prefix if type(new_prefix) is dict else {}, build)
            reboot_records, reboot_hashes = _chain(host, new_prefix, new_suffix)
            _replay_runtime_root(host, reboot_records, new_prefix, build)
            reboot_instance = _replay_module_instance(host, reboot_records, new_prefix)
            original_instance = _replay_module_instance(host, records, prefix)
            host.require(reboot_instance['instance'] != original_instance['instance'], 'BPF module instance was reused across boots')
            _replay_raw_evidence(host, reboot_records, reboot_records)
            _replay_exec_lifecycle(host, reboot_records)
            _replay_transition_sources(host, reboot_records, reboot)
            _replay_programs(host, reboot_records)
            _replay_ancestry(host, reboot_records, new_prefix, build)
            _replay_continuity(host, reboot_records, new_prefix, reboot_hashes, account, browser_image, scenario)
            _replay_audit(host, reboot_records)
            _replay_readbacks(host, reboot_records, reboot_hashes, new_prefix)
            _bootstrap_policy(host, reboot_records, new_prefix, policy_elf, target_btf)
            intent = result.get('root_journal', {}).get('reboot-intent.json')
            host.require(type(intent) is dict and _hex(intent.get('original_bpf_tail')) and
                         reboot.get('original_bpf_tail') == intent['original_bpf_tail'] and
                         intent['original_bpf_tail'] in hashes and new_prefix['boot_id'] != prefix['boot_id'] and
                         reboot.get('reboot', {}).get('boot_id') == new_prefix['boot_id'],
                         'BPF reboot original tail or new boot differs')
            checkpoint_index = hashes.index(intent['original_bpf_tail'])
            host.require(checkpoint_index > 0 and records[checkpoint_index - 1]['kind'] == 'checkpoint' and
                         records[checkpoint_index - 1]['body']['state'] == 'quiesced',
                         'BPF reboot intent does not bind its original quiesce record')
            containers = intent.get('containers')
            host.require(type(containers) is dict and set(containers) == {'app', 'w1', 'w2', 'control'},
                         'BPF reboot original container inventory missing')
            for name, item in containers.items():
                host.require(_shape(item, ('id', 'config')) and _hex(item['id']) and type(item['config']) is dict,
                             'BPF reboot container identity invalid')
                original = [event['body'] for event in records if event['kind'] == 'grant' and
                            event['body']['purpose'] == 'postgres-live-' + name and event['body']['container_id'] == item['id']]
                recovered = [event['body'] for event in reboot_records if event['kind'] == 'grant' and
                             event['body']['purpose'] == 'recovery-' + name and event['body']['container_id'] == item['id']]
                host.require(original and recovered and all(grant['config'] == item['config'] for grant in original + recovered),
                             'BPF reboot container config does not match original grants')
            observed = reboot.get('reboot', {}).get('observation')
            host.require(type(observed) is dict and observed.get('realm_available') is False and
                         observed.get('no_copy_reissuable') is False, 'BPF reboot reissued realm authority')
    except (KeyError, TypeError, ValueError, OverflowError, RecursionError, UnicodeError, OSError, ImportError, struct.error, tarfile.TarError) as exc:
        raise host.Failure('BPF evidence is malformed') from exc
