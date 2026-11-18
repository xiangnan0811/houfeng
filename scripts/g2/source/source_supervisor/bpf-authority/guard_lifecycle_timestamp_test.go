//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestGuardLifecycleTimestamp(t *testing.T) {
	if guardRecordSize != 192 || guardEventsSize != 12344 {
		t.Fatalf("guard wire geometry changed: record=%d events=%d", guardRecordSize, guardEventsSize)
	}
	for _, kind := range []uint32{2, 4, 5, 6} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			var raw [192]byte
			binary.LittleEndian.PutUint64(raw[0:8], 1)
			binary.LittleEndian.PutUint32(raw[8:12], kind)
			binary.LittleEndian.PutUint64(raw[88:96], 123456789)
			record, err := decodeGuardRecord(raw[:], 1)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := record.projection()
			if err != nil {
				t.Fatal(err)
			}
			detail, ok := projection["detail"].(map[string]any)
			if !ok || len(detail) != 1 || detail["observed_mono_ns"] != json.Number("123456789") {
				t.Fatalf("unexpected detail: %#v", projection["detail"])
			}
			zero := raw
			clear(zero[88:96])
			if _, err = decodeGuardRecord(zero[:], 1); err == nil {
				t.Fatal("accepted zero timestamp")
			}
			dirty := raw
			dirty[127] = 1
			if _, err = decodeGuardRecord(dirty[:], 1); err == nil {
				t.Fatal("accepted nonzero reserved detail")
			}
			record.Detail[39] = 1
			if _, err = record.projection(); err == nil {
				t.Fatal("projected nonzero reserved detail")
			}
		})
	}
	status := statusWireFixture(1, 1, true)
	if _, err := decodeGuardStatus(status); err != nil {
		t.Fatalf("valid V5 status rejected: %v", err)
	}
	binary.LittleEndian.PutUint32(status[:4], 4)
	if _, err := decodeGuardStatus(status); err == nil {
		t.Fatal("accepted V4 live status header")
	}
}
