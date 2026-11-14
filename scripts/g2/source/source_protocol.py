"""Source VM result transport, independent of the frozen phase-zero limits."""
import base64
import binascii
import hashlib
import json
import re
import time

MAX_RESULT_BYTES = 67108864
MAX_PROTOCOL = 134217728
MAX_EARLY_RESULT_CHUNKS = 8192


def emit(carrier):
    try:
        raw = json.dumps(carrier.RESULT, sort_keys=True, separators=(',', ':'),
                         ensure_ascii=True, allow_nan=False).encode('ascii')
        if len(raw) > MAX_RESULT_BYTES:
            from journal import JournalError
            raise JournalError('source result exceeds limit')
    except Exception:
        carrier.RESULT['completed'] = False
        raise
    deadline = time.monotonic() + 30
    count = (len(raw) + 2047) // 2048
    for index in range(count):
        chunk = raw[index * 2048:(index + 1) * 2048]
        carrier.emit_protocol('G2LAB-DATA %s %d/%d %s' %
                              (carrier.RESULT['run_token'], index, count,
                               base64.b64encode(chunk).decode('ascii')), deadline)
    carrier.emit_protocol('G2LAB-END %s %s' %
                          (carrier.RESULT['run_token'], hashlib.sha256(raw).hexdigest()), deadline)


def protocol_prefix(host, frozen, data, scenario, complete, expected_install):
    require = host.require
    token = host.TOKEN
    require(type(data) is bytes and len(data) <= MAX_PROTOCOL, 'invalid protocol bytes or size')
    parts = data.split(b'\n')
    partial = parts.pop()
    require(len(partial) < 8192 and (not complete or not partial), 'unterminated or oversized protocol line')
    ready, chunks, total, end = False, [], None, None
    installation = transport = None
    payload_size = 0
    for line in parts:
        require(len(line) + 1 <= 8192 and bool(line) and end is None and
                not (scenario == 'abort' and ready), 'empty, oversized, or trailing protocol line')
        if line == ('G2LAB-READY ' + token).encode():
            require(installation is not None and not ready and not chunks, 'unexpected or out-of-order READY')
            ready = True
            continue
        match = re.fullmatch(rb'G2LAB-INSTALL ([0-9a-f]{32}) ([0-9a-f]{64}) ([A-Za-z0-9+/]*={0,2})', line)
        if match:
            require(installation is None and not ready and match[1].decode() == token, 'unexpected or out-of-order INSTALL')
            try:
                raw = base64.b64decode(match[3], validate=True)
            except binascii.Error as exc:
                raise host.Failure('invalid installation base64') from exc
            require(len(raw) <= 4096 and base64.b64encode(raw) == match[3] and
                    hashlib.sha256(raw).hexdigest() == match[2].decode(), 'installation payload, base64, or checksum mismatch')
            try:
                envelope = host.strict_json(raw)
                require(host.canonical(envelope) == raw + b'\n', 'noncanonical installation JSON')
                require(type(envelope) is dict and set(envelope) == {'version', 'run_token', 'case', 'installation', 'transport'} and
                        type(envelope['version']) is int and envelope['version'] == 2 and
                        envelope['run_token'] == token and envelope['case'] == scenario, 'installation envelope identity mismatch')
                installation = envelope['installation']
                require(type(installation) is dict and installation == expected_install, 'guest installation differs')
                try:
                    transport = frozen.validate_transport(envelope['transport'])
                except frozen.Failure as exc:
                    raise host.Failure('invalid transport evidence: ' + str(exc)) from exc
            except (KeyError, TypeError, ValueError) as exc:
                raise host.Failure('invalid installation evidence: ' + str(exc)) from exc
            continue
        match = re.fullmatch(rb'G2LAB-DATA ([0-9a-f]{32}) ([0-9]+)/([0-9]+) ([A-Za-z0-9+/]*={0,2})', line)
        if match:
            require(scenario != 'abort' and ready and match[1].decode() == token, 'unexpected DATA')
            index, count = int(match[2]), int(match[3])
            require(0 < count <= 65536 and index < count and (total is None or total == count) and
                    index == len(chunks) and str(index).encode() == match[2] and str(count).encode() == match[3],
                    'duplicate or inconsistent DATA')
            try:
                raw = base64.b64decode(match[4], validate=True)
            except binascii.Error as exc:
                raise host.Failure('invalid base64') from exc
            require(raw and base64.b64encode(raw) == match[4], 'empty or noncanonical DATA')
            payload_size += len(raw)
            require(payload_size <= MAX_RESULT_BYTES, 'JSON limit exceeded')
            chunks.append(raw)
            total = count
            continue
        match = re.fullmatch(rb'G2LAB-END ([0-9a-f]{32}) ([0-9a-f]{64})', line)
        if match:
            require(scenario != 'abort' and ready and total is not None and len(chunks) == total and
                    match[1].decode() == token, 'unexpected or out-of-order END')
            end = match[2].decode()
            continue
        raise host.Failure('malformed protocol line')
    if partial and (end is not None or (scenario == 'abort' and ready)):
        raise host.Failure('trailing protocol bytes')
    if complete:
        require(installation is not None and transport is not None and ready, 'missing INSTALL or READY')
        require((scenario == 'abort' and not chunks and end is None) or
                (scenario != 'abort' and end is not None and total is not None and len(chunks) == total),
                'missing result chunks or END, or unexpected abort result')
    return {'installation': installation, 'transport': transport, 'ready': ready, 'chunks': chunks, 'end': end}
