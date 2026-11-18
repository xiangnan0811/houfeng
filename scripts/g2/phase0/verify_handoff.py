"""Conservative verification and classification for the input-handoff run."""
import json
import os


def default_cases(m):
    return {case: {profile: {'status': 'NOT_RUN', 'observations': dict(m.EXPERIMENT_BOUNDARY)} for profile in ('A', 'B')}
            for case in m.EXPERIMENT_CASES}


def default_preparation_checks(m):
    return {variant: {profile: {'status': 'NOT_RUN', 'observations': dict(m.EXPERIMENT_BOUNDARY)}
                      for profile in ('A', 'B')} for variant in m.PREPARATION_VARIANTS}


def group_status(cases, names, checks=None, variants=()):
    values = [cases[name][profile]['status'] for name in names for profile in ('A', 'B')]
    if checks is not None:
        values.extend(checks[name][profile]['status'] for name in variants for profile in ('A', 'B'))
    if all(value == 'PASS' for value in values):
        return 'PASS'
    if any(value == 'INCONCLUSIVE' for value in values):
        return 'INCONCLUSIVE'
    if any(value == 'FAIL' for value in values):
        return 'FAIL'
    if any(value == 'PASS' for value in values):
        return 'INCONCLUSIVE'
    return 'NOT_RUN'


def verify(m):
    report = {'experiment_kind': 'input-handoff-v1', 'run_token': m.TOKEN,
              'carrier': {'status': 'NOT_RUN', 'stages': {}},
              'matrix_execution': {'status': 'NOT_RUN'}, 'cases': default_cases(m),
              'preparation_version': 1, 'preparation_checks': default_preparation_checks(m),
              'input_compatibility': 'NOT_RUN', 'running_object_handoff': 'NOT_RUN',
              'mount_namespace_identity': 'UNPROVEN', 'all_overlay_layers': 'UNPROVEN',
              'product_gate': 'NOT_READY', 'uncertain': False,
              'diagnostic': {'status': 'NOT_RUN', 'reasons': ['no abort receipt'], 'snapshots': []}}
    report['reader_faults'] = {name: {'status': 'NOT_RUN'} for name in m.READER_FAULT_SCENARIOS}
    paths = {'prepare': m.RUN / 'prepare.json', 'build': m.RUN / 'build.json',
             'abort': m.RUN / 'runs/abort/receipt.json', 'matrix': m.RUN / 'runs/matrix/receipt.json'}
    objects = {}
    for name, path in paths.items():
        stage = report['carrier']['stages'][name] = {'status': 'NOT_RUN'}
        if not os.path.lexists(path):
            continue
        try:
            value = m.load_json(path)
            m.require(isinstance(value, dict) and value.get('run_token') == m.TOKEN and
                      value.get('experiment_kind') == 'input-handoff-v1' and
                      all(value.get(key) == expected for key, expected in m.EXPERIMENT_BOUNDARY.items()),
                      'wrong receipt identity or capability boundary')
            status = value.get('status')
            m.require(status in ('PASS', 'FAIL', 'IN_PROGRESS', 'PREREQUISITE_FAILED'), 'invalid receipt status')
            if name in ('abort', 'matrix'):
                m.require(value.get('case') == name, 'receipt case mismatch')
            stage.update(status=status)
            if 'error' in value:
                stage['error'] = value['error']
            objects[name] = value
        except (OSError, ValueError, m.Failure, KeyError, TypeError) as exc:
            stage.update(status='FAIL', error=str(exc))
    abort = objects.get('abort')
    if abort and abort.get('status') != 'PASS' and abort.get('seed'):
        try:
            report['diagnostic'] = m.diagnostic_report(m.RUN / 'runs/abort', abort)
        except (OSError, ValueError, m.Failure, KeyError, TypeError) as exc:
            report['diagnostic'] = {'status': 'FAIL', 'reasons': [str(exc)], 'snapshots': []}
    checking = 'prepare'
    try:
        if report['carrier']['stages']['prepare']['status'] == 'PASS':
            m.prepare_import.gate(m)
        checking = 'build'
        if report['carrier']['stages']['build']['status'] == 'PASS':
            result, build = m.build_gate()
            for case in ('abort', 'matrix'):
                checking = case
                receipt = objects.get(case)
                if not receipt:
                    continue
                stage_status = report['carrier']['stages'][case]['status']
                if stage_status != 'PASS' and (case != 'matrix' or 'parsed_sha256' not in receipt):
                    continue
                directory = m.RUN / 'runs' / case
                m.require(receipt.get('exit_observed') is True and receipt.get('base_unchanged') is True and
                          receipt.get('seed_unchanged') is True and
                          receipt.get('base_before') == m.UBUNTU_HASHES[m.IMAGE] and
                          receipt.get('base_after') == m.UBUNTU_HASHES[m.IMAGE] and
                          receipt.get('build_sha256') == m.file_hash(m.RUN / 'build.json') and
                          receipt.get('seed') == m.case_seed_hashes(case, build['seed']),
                          case + ' owned exit or immutable input evidence missing')
                bundle = m.verify_vm_outputs(directory, receipt, case)
                parsed = bundle['result']
                if case == 'abort':
                    m.require(parsed is None, 'abort unexpectedly contains a result')
                    diagnostic = m.diagnostic_report(directory, receipt)
                    report['diagnostic'] = diagnostic
                    m.require(diagnostic.get('status') == 'PASS' and diagnostic == receipt.get('diagnostic'),
                              'abort owned-exit diagnostic invalid')
                else:
                    m.require(report['carrier']['stages']['abort']['status'] == 'PASS' and
                              report['diagnostic'].get('status') == 'PASS',
                              'matrix evidence lacks a verified abort exit')
                    m.require(m.file_hash(directory / 'parsed.json') == receipt['parsed_sha256'] and
                              m.load_json(directory / 'parsed.json') == parsed, 'matrix parsed evidence changed')
                    report['cases'] = parsed['cases']
                    report['preparation_checks'] = parsed['preparation_checks']
                    report['uncertain'] = parsed['uncertain']
                    report['carrier']['isolation'] = parsed['isolation']
                    report['carrier']['versions'] = parsed['versions']
                    if parsed['uncertain']:
                        report['matrix_execution'] = {'status': 'UNCERTAIN', 'reason': 'ownership_or_exit_unknown'}
                    elif stage_status == 'PASS' and parsed['completed'] is True:
                        report['matrix_execution'] = {'status': 'PASS'}
                    elif parsed['completed'] is False:
                        report['matrix_execution'] = {'status': 'INCONCLUSIVE', 'reason': 'guest_incomplete'}
                    else:
                        report['matrix_execution'] = {'status': 'FAIL', 'reason': 'matrix_carrier_failed'}
            m.require(m.file_hash(m.Path(result['hash_checks']['ubuntu:' + m.IMAGE]['path'])) ==
                      m.UBUNTU_HASHES[m.IMAGE], 'base image changed')
            for scenario in m.READER_FAULT_SCENARIOS:
                case = 'reader-fault-' + scenario
                directory = m.RUN / 'runs' / case
                if not os.path.lexists(directory / 'receipt.json'):
                    continue
                try:
                    receipt = m.load_json(directory / 'receipt.json')
                    m.require(receipt.get('run_token') == m.TOKEN and receipt.get('case') == case and
                              receipt.get('status') in ('PASS', 'FAIL') and
                              receipt.get('exit_observed') is True and receipt.get('base_unchanged') is True and
                              receipt.get('seed_unchanged') is True and
                              receipt.get('base_before') == m.UBUNTU_HASHES[m.IMAGE] and
                              receipt.get('base_after') == m.UBUNTU_HASHES[m.IMAGE] and
                              receipt.get('build_sha256') == m.file_hash(m.RUN / 'build.json') and
                              receipt.get('seed') == m.case_seed_hashes(case, build['seed']),
                              'fault VM owned exit or immutable input evidence missing')
                    m.require(report['carrier']['stages']['abort']['status'] == 'PASS' and
                              report['diagnostic'].get('status') == 'PASS', 'fault VM lacks verified abort')
                    parsed = m.verify_vm_outputs(directory, receipt, case)['result']
                    m.require(m.file_hash(directory / 'parsed.json') == receipt.get('parsed_sha256') and
                              m.load_json(directory / 'parsed.json') == parsed, 'fault VM result binding')
                    if receipt['status'] == 'PASS':
                        m.require(parsed['outcome'] == 'EXPECTED_UNCERTAIN' and
                                  receipt.get('fault_validation') == 'PASS' and
                                  receipt.get('guest_outcome') == 'UNCERTAIN', 'fault expectation invalid')
                        report['reader_faults'][scenario] = {'status': 'PASS', 'guest_outcome': 'UNCERTAIN',
                                                             'receipt_sha256': m.file_hash(directory / 'receipt.json')}
                    else:
                        report['reader_faults'][scenario] = {'status': 'FAIL', 'guest_outcome': parsed['outcome'],
                                                             'failure_stage': parsed['failure_stage'],
                                                             'error': parsed['error'] or receipt.get('error')}
                except (OSError, ValueError, m.Failure, KeyError, TypeError) as exc:
                    report['reader_faults'][scenario] = {'status': 'FAIL', 'error': str(exc)}
    except (OSError, ValueError, m.Failure, KeyError, TypeError) as exc:
        report['carrier']['error'] = str(exc)
        report['carrier']['stages'][checking] = {'status': 'FAIL', 'error': str(exc)}
        if checking == 'matrix' or objects.get('matrix'):
            report['matrix_execution'] = {'status': 'INCONCLUSIVE', 'reason': 'evidence_invalid'}
    stages = report['carrier']['stages']
    carrier_values = [stages[name]['status'] for name in ('prepare', 'build', 'abort')]
    if all(status == 'PASS' for status in carrier_values):
        report['carrier']['status'] = 'PASS'
    elif any(status in ('FAIL', 'PREREQUISITE_FAILED') for status in carrier_values):
        report['carrier']['status'] = ('PREREQUISITE_FAILED' if any(stages[x]['status'] in ('FAIL', 'PREREQUISITE_FAILED')
                                      for x in ('prepare', 'build')) else 'FAIL')
    elif any(status != 'NOT_RUN' for status in carrier_values):
        report['carrier']['status'] = 'INCONCLUSIVE'
    matrix_stage = stages['matrix']['status']
    if report['matrix_execution']['status'] == 'NOT_RUN' and matrix_stage in ('FAIL', 'PREREQUISITE_FAILED'):
        report['matrix_execution'] = {'status': 'FAIL', 'reason': 'matrix_carrier_failed'}
    elif report['matrix_execution']['status'] == 'NOT_RUN' and matrix_stage == 'IN_PROGRESS':
        report['matrix_execution'] = {'status': 'INCONCLUSIVE', 'reason': 'matrix_in_progress'}
    checks = report['preparation_checks']
    report['input_compatibility'] = group_status(report['cases'], ('C0', 'V1', 'V2', 'V3'), checks,
                                                  m.OBSERVER_NEGATIVES + m.MALFORMED_NEGATIVES)
    report['running_object_handoff'] = group_status(report['cases'], ('C0', 'I0', 'I1', 'I2', 'I3'), checks,
                                                    m.KILL_NEGATIVES)
    fault_statuses = [value['status'] for value in report['reader_faults'].values()]
    report['reader_fault_acceptance'] = ('PASS' if all(value == 'PASS' for value in fault_statuses) else
                                         'FAIL' if 'FAIL' in fault_statuses else 'NOT_RUN')
    if report['running_object_handoff'] == 'PASS' and report['reader_fault_acceptance'] != 'PASS':
        report['running_object_handoff'] = ('FAIL' if report['reader_fault_acceptance'] == 'FAIL' else 'INCONCLUSIVE')
    def blocked_by_failure(case, profile, seen=()):
        if case in seen or case not in report['cases']:
            return False
        item = report['cases'][case][profile]
        if item['status'] == 'FAIL':
            return True
        observation = item['observations']
        return (item['status'] == 'NOT_RUN' and observation.get('reason') == 'dependency_not_passed' and
                blocked_by_failure(observation.get('dependency'), profile, seen + (case,)))

    all_items = [report['cases'][case][profile] for case in m.EXPERIMENT_CASES for profile in ('A', 'B')]
    all_items += [checks[name][profile] for name in m.PREPARATION_VARIANTS for profile in ('A', 'B')]
    unknown = any(item['status'] == 'INCONCLUSIVE' for item in all_items)
    for case in m.EXPERIMENT_CASES:
        for profile in ('A', 'B'):
            item = report['cases'][case][profile]
            if item['status'] == 'NOT_RUN' and not blocked_by_failure(case, profile):
                unknown = True
    for name in m.PREPARATION_VARIANTS:
        for profile in ('A', 'B'):
            item = checks[name][profile]
            dependencies = ('I0', 'I1') if name in m.KILL_NEGATIVES else ('C0',)
            if item['status'] == 'NOT_RUN' and not any(blocked_by_failure(dep, profile) for dep in dependencies):
                unknown = True
    incomplete = (report['uncertain'] or report['carrier']['status'] != 'PASS' or
                  report['matrix_execution']['status'] != 'PASS' or unknown or 'NOT_RUN' in fault_statuses)
    failed = any(item['status'] == 'FAIL' for item in all_items) or 'FAIL' in fault_statuses
    print(json.dumps(report, indent=2, sort_keys=True))
    return 2 if incomplete else 1 if failed else 0
