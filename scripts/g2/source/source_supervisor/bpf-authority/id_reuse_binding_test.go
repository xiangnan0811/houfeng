//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"testing"
)

func TestIDReuseBinding(t *testing.T) {
	t.Run("pinned kernel binding", func(t *testing.T) {
		const script = `
import struct
import source_kernel_observer as obs
from source_host import RUN
p = RUN / 'source-build-attempts/attempt-00000000000000000103/boot-inputs'
boot = p / 'boot'
kernel, smap, config = ((boot / name).read_bytes() for name in ('vmlinuz', 'System.map', 'config'))
symvers = (p / 'headers/usr/src/linux-headers-6.8.0-147-generic/Module.symvers').read_bytes()
args = (kernel, smap, config, 'e3a255c540801b0b70de6705b6a34a02ae9bdde9a9348cb047217b019a0fe14f', symvers)
binding = obs.observer_bindings(*args)
layout = binding['layout']
assert (layout['idr_base_offset'], layout['idr_next_offset'], layout['spinlock_size']) == (16, 20, 4)
symbols = {s['name']: s for s in binding['symbols']}
assert symbols['map_idr_lock']['type'] == 'b'
windows = binding['allocator_windows']
assert len(windows) == 2 and [w['relocations'] for w in windows] == [[3, 34, 57], [3, 18, 43]]
for w in windows:
    b = bytes.fromhex(w['bytes'])
    for pos, symbol in zip(w['relocations'], ('map_idr_lock', 'map_idr', 'map_idr_lock')):
        assert b[pos-3:pos] == b'\x48\xc7\xc7'
        assert struct.unpack_from('<I', b, pos)[0] == ((symbols[symbol]['delta'] + int(next(row.split()[0] for row in smap.decode().splitlines() if row.endswith(' T idr_alloc_cyclic')), 16)) & 0xffffffff)
header = obs.generated_header(binding)
assert b'G2_OBS_LAYOUT_IDR_NEXT_OFFSET (20U)' in header and b'g2_observer_windows' in header
original = obs._read_address
decoded = obs.decode_kernel(kernel)
anchor = int(next(row.split()[0] for row in smap.decode().splitlines() if row.endswith(' T idr_alloc_cyclic')), 16)
target = anchor + windows[0]['delta']
def corrupt(d, address, size):
    data = original(d, address, size)
    if address == target and size == len(bytes.fromhex(windows[0]['bytes'])):
        data = bytes([data[0] ^ 1]) + data[1:]
    return data
obs._read_address = corrupt
try:
    obs.observer_bindings(*args)
except ValueError:
    pass
else:
    raise AssertionError('corrupt allocator window accepted')
`
		cmd := exec.Command("python3", "-B", "-c", script)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pinned kernel binding: %v\n%s", err, out)
		}
	})
	t.Run("request ABI", func(t *testing.T) {
		nonce := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		b, err := guardIDReuseMessage(0x102, 0x304, 0x506, nonce)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]byte, 48)
		binary.LittleEndian.PutUint32(want[0:4], 4)
		binary.LittleEndian.PutUint64(want[8:16], 0x102)
		binary.LittleEndian.PutUint64(want[16:24], 0x304)
		binary.LittleEndian.PutUint64(want[24:32], 0x506)
		copy(want[32:48], nonce[:])
		if !bytes.Equal(b, want) || guardIOCTL(14, 48) != 0xc030b70e || guardRecordSize != 192 || guardEventsSize != 12344 {
			t.Fatalf("wrong request ABI: %x ioctl=%x", b, guardIOCTL(14, 48))
		}
		for _, args := range []struct {
			cookie, seq, tx uint64
			nonce           [16]byte
		}{
			{0, 1, 1, nonce}, {1, 0, 1, nonce}, {1, 1, 0, nonce}, {1, 1, 1, [16]byte{}},
		} {
			if _, err := guardIDReuseMessage(args.cookie, args.seq, args.tx, args.nonce); err == nil {
				t.Fatal("accepted missing identity")
			}
		}
	})

	makeRecord := func(op uint32) guardRecord {
		r := guardRecord{Sequence: 99, Kind: 16, ObjectCookie: 5, KernelID: 23, TaskCookie: 7, TaskStartNS: 8, Operation: op}
		binary.LittleEndian.PutUint64(r.Detail[0:8], 30)
		binary.LittleEndian.PutUint64(r.Detail[8:16], 40)
		binary.LittleEndian.PutUint32(r.Detail[20:24], 23)
		if op >= 2 {
			binary.LittleEndian.PutUint32(r.Detail[16:20], 81)
		}
		if op == 2 {
			binary.LittleEndian.PutUint32(r.Detail[28:32], 23)
		}
		if op == 3 {
			binary.LittleEndian.PutUint32(r.Detail[24:28], uint32(0xfffffff4))
			binary.LittleEndian.PutUint32(r.Detail[28:32], 81)
			r.Result = -12
		}
		return r
	}
	t.Run("cursor records", func(t *testing.T) {
		for op := uint32(1); op <= 3; op++ {
			r := makeRecord(op)
			projected, err := r.projection()
			if err != nil {
				t.Fatalf("operation %d: %v", op, err)
			}
			detail, ok := projected["detail"].(map[string]any)
			if !ok || len(detail) != 6 || detail["first_removal_sequence"] != jsonU64(30) ||
				detail["transaction_id"] != jsonU64(40) || detail["requested_id"] != jsonU64(23) {
				t.Fatalf("operation %d projection: %#v", op, detail)
			}
			if op == 3 && (detail["allocator_result"] != jsonI64(-12) || detail["cursor_after"] != jsonU64(81)) {
				t.Fatalf("lost allocator result or restoration: %#v", detail)
			}
		}
	})
	t.Run("malformed cursor records", func(t *testing.T) {
		cases := map[string]func(*guardRecord){
			"zero cookie":         func(r *guardRecord) { r.ObjectCookie = 0 },
			"wrong kernel ID":     func(r *guardRecord) { r.KernelID++ },
			"missing actor":       func(r *guardRecord) { r.TaskCookie = 0 },
			"flags":               func(r *guardRecord) { r.Flags = 2 },
			"reserved":            func(r *guardRecord) { r.Detail[32] = 1 },
			"snapshot":            func(r *guardRecord) { r.Snapshot[0] = 1 },
			"missing removal":     func(r *guardRecord) { clear(r.Detail[0:8]) },
			"missing transaction": func(r *guardRecord) { clear(r.Detail[8:16]) },
			"unknown operation":   func(r *guardRecord) { r.Operation = 4 },
			"arm cursor":          func(r *guardRecord) { r.Detail[16] = 1 },
		}
		for name, mutate := range cases {
			t.Run(name, func(t *testing.T) {
				r := makeRecord(1)
				mutate(&r)
				if _, err := r.projection(); err == nil {
					t.Fatal("accepted malformed cursor record")
				}
			})
		}
		r := makeRecord(3)
		r.Detail[28]++
		if _, err := r.projection(); err == nil {
			t.Fatal("accepted unrestored cursor")
		}
		r = makeRecord(3)
		r.Result = 0
		if _, err := r.projection(); err == nil {
			t.Fatal("accepted inconsistent allocator result")
		}
	})
}
