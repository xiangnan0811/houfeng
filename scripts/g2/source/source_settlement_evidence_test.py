"""Wire and conservative summary checks for settlement diagnostics."""
import base64
import copy
import struct
import unittest

import source_settlement_evidence as evidence


def snapshot(phase, *, observed=False, broker_ns=150, native_ns=90, nonce=b'\x03' * 16):
    raw = bytearray(4096)
    struct.pack_into('<8sHBBI', raw, 0, b'G2SEV1\0\0', 1, phase, 1, 64)
    fields = [1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 10, 100, 1000, 100 if phase == 3 else 0, 160]
    struct.pack_into('<15Q', raw, 16, *fields)
    raw[136:152], raw[152:168], raw[168:184] = b'\x01' * 16, b'\x02' * 16, nonce
    struct.pack_into('<I', raw, 248, 1 if phase == 3 else 0)
    struct.pack_into('<Q', raw, 3328 + 24 * 8, 64)
    struct.pack_into('<Q', raw, 3328 + 25 * 8, 384)
    struct.pack_into('<Q', raw, 3328 + 29 * 8, 5)
    if observed:
        struct.pack_into('<4Q', raw, 16 + 3 * 8, 4, 5, 6, 4)
        struct.pack_into('<II', raw, 216, 1, 1)
        struct.pack_into('<8Q', raw, 2816, 2, 4, 5, 6, 4, 0, 0, 0)
        struct.pack_into('<8Q', raw, 2880, 2, 0, 0, 0, 0, 0, 0, 0)
        struct.pack_into('<9QII', raw, 256 + 24 * 80, 7, 7, native_ns, native_ns, broker_ns,
                         broker_ns, 1, 7, 0, 1, 8 if broker_ns > 100 else 0)
    return bytes(raw)


def envelope(*frames, collection='COMPLETE'):
    return {'version': 1, 'probes': [{'purpose': 'postgres-uid', 'nonce': '03' * 16,
        'collection': collection, 'guest': {'operation_start_ns': 10, 'operation_deadline_ns': 100,
        'authority_deadline_ns': 1000, 'ack_received_ns': 0},
        'snapshots': [base64.b64encode(frame).decode('ascii') for frame in frames],
        'reference_holder': 'UNRESOLVED'}]}


class SettlementEvidenceTest(unittest.TestCase):
    def test_legacy_and_empty_native_are_unknown(self):
        self.assertEqual(evidence.summarize_evidence(None)['quality'], 'UNAVAILABLE')
        value = envelope(snapshot(1), snapshot(3))
        self.assertEqual(evidence.summarize_evidence(value)['delivery'], 'UNKNOWN')
        self.assertEqual(evidence.summarize_evidence(value)['last_native_stage'], 'UNKNOWN')

    def test_native_time_is_not_broker_read_time(self):
        value = envelope(snapshot(1), snapshot(3, observed=True))
        summary = evidence.summarize_evidence(value)
        self.assertEqual(summary['last_native_stage'], 'ID_REMOVAL_RETURN')
        self.assertEqual(summary['delivery'], 'UNKNOWN')
        self.assertEqual(summary['timeliness'], 'BEFORE_DEADLINE')
        self.assertEqual(summary['reference_holder'], 'UNRESOLVED')

    def test_delivery_uses_matching_pipeline_facts(self):
        raw = bytearray(snapshot(3, observed=True, broker_ns=95))
        def put(slot, value, sequence=7, native=0, reason=0):
            struct.pack_into('<9QII', raw, 256 + slot * 80, sequence, sequence, native, native,
                             95, 95, 1, value, reason, 1, 0)
        def delivery():
            return evidence.summarize_evidence(envelope(snapshot(1), bytes(raw)))['delivery']
        self.assertEqual(delivery(), 'NOT_ADMITTED')
        put(25, 7, native=90)
        self.assertEqual(delivery(), 'ADMITTED')
        put(26, 2, reason=9)
        self.assertEqual(delivery(), 'REJECTED')
        put(26, 1)
        put(27, 1)
        put(28, 2)
        put(29, 7)
        self.assertEqual(delivery(), 'ACCEPTED')

    def test_mixed_native_cutoff_is_unknown(self):
        raw = bytearray(snapshot(3, observed=True, broker_ns=95, native_ns=90))
        offset = 256 + 24 * 80
        struct.pack_into('<Q', raw, offset + 8, 8)
        struct.pack_into('<Q', raw, offset + 24, 110)
        struct.pack_into('<Q', raw, offset + 48, 2)
        struct.pack_into('<I', raw, offset + 76, 8)
        summary = evidence.summarize_evidence(envelope(snapshot(1), bytes(raw)))
        self.assertEqual(summary['timeliness'], 'UNKNOWN')

    def test_wire_rejects_reserved_versions_and_bounds(self):
        for offset, byte in ((8, 2), (252, 1), (3840, 1), (3328 + 47 * 8, 1)):
            raw = bytearray(snapshot(1))
            raw[offset] = byte
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                evidence.decode_snapshot(bytes(raw))
        with self.assertRaises(ValueError):
            evidence.decode_snapshot(snapshot(1)[:-1])

    def test_identity_order_and_schema_are_strict(self):
        base = envelope(snapshot(1), snapshot(3))
        for mutate in ('nonce', 'boot', 'phase', 'bool', 'extra', 'base64'):
            value = copy.deepcopy(base)
            if mutate == 'nonce':
                value['probes'][0]['nonce'] = '04' * 16
            elif mutate == 'boot':
                raw = bytearray(snapshot(3)); raw[136] = 9
                value['probes'][0]['snapshots'][1] = base64.b64encode(raw).decode('ascii')
            elif mutate == 'phase':
                value['probes'][0]['snapshots'].reverse()
            elif mutate == 'bool':
                value['probes'][0]['guest']['ack_received_ns'] = True
            elif mutate == 'extra':
                value['probes'][0]['extra'] = 0
            else:
                value['probes'][0]['snapshots'][0] += '='
            with self.subTest(mutate=mutate), self.assertRaises(ValueError):
                evidence.validate_evidence(value)
    def test_cutoff_and_status_require_consistent_flags(self):
        raw = bytearray(snapshot(3, observed=True))
        struct.pack_into('<I', raw, 256 + 24 * 80 + 76, 0)
        with self.assertRaises(ValueError):
            evidence.decode_snapshot(bytes(raw))
        raw = bytearray(snapshot(1))
        struct.pack_into('<Q', raw, 3328 + 2 * 8, 3)
        with self.assertRaises(ValueError):
            evidence.decode_snapshot(bytes(raw))

    def test_incarnation_and_capture_cannot_drift(self):
        first, final = bytearray(snapshot(1)), bytearray(snapshot(3))
        struct.pack_into('<Q', first, 16 + 4 * 8, 9)
        struct.pack_into('<Q', final, 16 + 4 * 8, 10)
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(bytes(first), bytes(final)))
        first, final = bytearray(snapshot(1)), bytearray(snapshot(3))
        struct.pack_into('<Q', first, 16 + 14 * 8, 170)
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(bytes(first), bytes(final)))

    def test_same_sequence_cannot_change_native_time(self):
        first = snapshot(1, observed=True, broker_ns=95, native_ns=90)
        final = snapshot(3, observed=True, broker_ns=95, native_ns=91)
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(first, final))

    def test_identity_may_bind_once_but_never_change(self):
        first, wait, final = bytearray(snapshot(1)), bytearray(snapshot(2)), bytearray(snapshot(3))
        wait[184:216] = final[184:216] = b'\x11' * 32
        evidence.validate_evidence(envelope(bytes(first), bytes(wait), bytes(final)))
        final[184:216] = b'\x22' * 32
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(bytes(first), bytes(wait), bytes(final)))

    def test_duplicate_and_export_gap_cannot_be_complete(self):
        first = snapshot(1)
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(first, first))
        final = bytearray(snapshot(3))
        struct.pack_into('<I', final, 12, 64 | 128)
        struct.pack_into('<Q', final, 3328 + 24 * 8, 64 | 128)
        struct.pack_into('<Q', final, 3328 + 28 * 8, 2)
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(first, bytes(final)))
        value = envelope(first, bytes(final), collection='PARTIAL')
        self.assertEqual(evidence.summarize_evidence(value)['quality'], 'PARTIAL')

    def test_unknown_base64_and_enum_are_rejected(self):
        for collection in ('OBSERVED', '', 1, None):
            value = envelope(snapshot(1), snapshot(3))
            value['probes'][0]['collection'] = collection
            with self.assertRaises(ValueError):
                evidence.validate_evidence(value)
        value = envelope(snapshot(1), snapshot(3))
        value['probes'][0]['snapshots'][0] = '!'
        with self.assertRaises(ValueError):
            evidence.validate_evidence(value)


    def test_saturation_and_eagain_preserve_uncertainty(self):
        raw = bytearray(snapshot(3))
        struct.pack_into('<I', raw, 12, 64 | 8)
        struct.pack_into('<Q', raw, 3328 + 24 * 8, 64 | 8)
        struct.pack_into('<Q', raw, 3328 + 31 * 8, 2)
        struct.pack_into('<2Q', raw, 3328 + 33 * 8, 25, 30)
        struct.pack_into('<9QII', raw, 256 + 30 * 80, 0, 0, 0, 0, 0, 0,
                         (1 << 64) - 1, 0, 0, 4, 4)
        value = envelope(snapshot(1), bytes(raw))
        summary = evidence.summarize_evidence(value)
        self.assertEqual(summary['delivery'], 'UNKNOWN')
        self.assertEqual(summary['last_native_stage'], 'UNKNOWN')
        self.assertEqual(summary['timeliness'], 'UNKNOWN')
        struct.pack_into('<I', raw, 256 + 30 * 80 + 76, 0)
        with self.assertRaises(ValueError):
            evidence.decode_snapshot(bytes(raw))

    def test_late_guest_ack_does_not_change_native_timeliness(self):
        value = envelope(snapshot(1), snapshot(3, observed=True, broker_ns=95, native_ns=90))
        value['probes'][0]['guest']['ack_received_ns'] = 1000
        self.assertEqual(evidence.summarize_evidence(value)['timeliness'], 'BEFORE_DEADLINE')

    def test_wait_pin_is_stable_until_final(self):
        enrolled = snapshot(1)
        wait = snapshot(2, observed=True, broker_ns=95)
        final = bytearray(snapshot(3, observed=True, broker_ns=95))
        evidence.validate_evidence(envelope(enrolled, wait, bytes(final)))
        struct.pack_into('<Q', final, 16 + 4 * 8, 17)
        struct.pack_into('<Q', final, 2816 + 2 * 8, 17)
        with self.assertRaises(ValueError):
            evidence.validate_evidence(envelope(enrolled, wait, bytes(final)))

    def test_both_probes_share_boot_epoch_and_owner(self):
        value = envelope(snapshot(1), snapshot(3))
        gid = copy.deepcopy(value['probes'][0])
        gid['purpose'], gid['nonce'] = 'postgres-gid', '04' * 16
        packets = []
        for phase in (1, 3):
            raw = bytearray(snapshot(phase, nonce=b'\x04' * 16))
            raw[11] = 2
            struct.pack_into('<Q', raw, 24, 3)
            packets.append(base64.b64encode(raw).decode('ascii'))
        gid['snapshots'] = packets
        value['probes'].append(gid)
        evidence.validate_evidence(value)
        raw = bytearray(base64.b64decode(gid['snapshots'][1]))
        raw[136] ^= 1
        gid['snapshots'][1] = base64.b64encode(raw).decode('ascii')
        with self.assertRaises(ValueError):
            evidence.validate_evidence(value)

    def test_only_actually_sampled_pending_points_are_reported(self):
        labels = ('EFFECT_OPERATION', 'ALIAS_EXECUTION', 'OBJECT_CLEANUP_STARTED',
                  'CGROUP_TARGET_UNOBSERVED', 'CGROUP_RETIRE_EAGAIN', 'CGROUP_TARGET_RELEASE',
                  'INVOCATION', 'EXEC_ADMISSION', 'TRANSACTION_RETIREMENT', 'OBJECT_REMOVAL',
                  'ATTACHMENT_RELEASE', 'OWNER_PROBE_RESULT', 'AUDIT_JOIN')
        for index, label in enumerate(labels):
            raw = bytearray(snapshot(3))
            mask = 1 << index
            struct.pack_into('<9QII', raw, 256 + 6 * 80, 0, 0, 0, 0, 95, 95, 1, mask, mask, 1, 0)
            struct.pack_into('<Q', raw, 3328 + 22 * 8, mask)
            struct.pack_into('<Q', raw, 3328 + 23 * 8, mask)
            self.assertEqual(evidence.summarize_evidence(envelope(snapshot(1), bytes(raw)))['blocking_points'], [label])
            struct.pack_into('<Q', raw, 3328 + 22 * 8, 0)
            with self.assertRaises(ValueError):
                evidence.decode_snapshot(bytes(raw))

    def test_missing_final_is_partial(self):
        value = envelope(snapshot(1), collection='PARTIAL')
        self.assertEqual(evidence.summarize_evidence(value)['quality'], 'PARTIAL')
        value['probes'][0]['collection'] = 'COMPLETE'
        with self.assertRaises(ValueError):
            evidence.validate_evidence(value)


if __name__ == '__main__':
    unittest.main()
