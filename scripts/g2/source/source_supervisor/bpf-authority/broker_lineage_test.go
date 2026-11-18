//go:build linux && amd64 && !cgo

package main

import "testing"

func managedLineageRecord(seq uint64, kind uint32, cookie, parent uint64, tgid, tid uint32, ino, generation uint64) guardRecord {
	r := guardRecord{Sequence: seq, Kind: kind, TaskCookie: cookie, ParentCookie: parent, TaskStartNS: cookie * 100}
	guardPut32(r.Detail[:], 0, tgid)
	guardPut32(r.Detail[:], 4, tid)
	guardPut64(r.Detail[:], 8, 1)
	guardPut64(r.Detail[:], 16, ino)
	guardPut64(r.Detail[:], 24, generation)
	return r
}

func TestManagedContainerdLineage(t *testing.T) {
	newLineage := func() *daemonLineage {
		l := &daemonLineage{executables: map[executableKey]lineageExecutable{}}
		for ino, role := range map[uint64]string{1: "helper", 2: "dockerd", 3: "containerd_launcher", 4: "containerd", 5: "shim", 6: "wrapper", 7: "runc"} {
			l.executables[executableKey{1, ino}] = lineageExecutable{role: role, digest: role}
		}
		return l
	}
	base := []guardRecord{
		managedLineageRecord(1, 12, 1, 99, 100, 100, 1, 1),
		managedLineageRecord(2, 10, 1, 99, 100, 100, 2, 2),
		managedLineageRecord(3, 9, 2, 1, 200, 200, 2, 2),
		managedLineageRecord(4, 10, 2, 1, 200, 200, 3, 3),
		managedLineageRecord(5, 10, 2, 1, 200, 200, 4, 4),
		managedLineageRecord(6, 9, 3, 2, 300, 300, 4, 4),
		managedLineageRecord(7, 10, 3, 2, 300, 300, 5, 5),
		managedLineageRecord(8, 9, 4, 3, 400, 400, 5, 5),
		managedLineageRecord(9, 10, 4, 3, 400, 400, 6, 6),
	}
	l := newLineage()
	for _, r := range base {
		if err := l.observe(r); err != nil {
			t.Fatalf("valid guard %d: %v", r.Sequence, err)
		}
	}
	if l.nodes[2].tainted || !l.nodes[2].managedContainerd {
		t.Fatal("managed transition rejected")
	}
	if !l.nodes[3].managedContainerd {
		t.Fatal("verified managed provenance not inherited by fork")
	}
	if _, err := l.ancestry(4, "wrapper"); err != nil {
		t.Fatalf("valid ancestry: %v", err)
	}
	if err := l.observe(managedLineageRecord(10, 10, 2, 1, 200, 200, 4, 5)); err != nil || l.nodes[2].tainted {
		t.Fatal("managed self exec rejected")
	}
	for _, tc := range []struct {
		name    string
		records []guardRecord
		target  uint64
	}{
		{"direct-target", []guardRecord{base[0], base[1], base[2], managedLineageRecord(4, 10, 2, 1, 200, 200, 4, 3)}, 2},
		{"root-launcher", []guardRecord{base[0], managedLineageRecord(2, 10, 1, 99, 100, 100, 3, 2)}, 1},
		{"launcher-self", []guardRecord{base[0], base[1], base[2], base[3], managedLineageRecord(5, 10, 2, 1, 200, 200, 3, 4)}, 2},
		{"fork-inherits-launcher", []guardRecord{base[0], base[1], base[2], base[3], managedLineageRecord(5, 9, 5, 2, 500, 500, 3, 3), managedLineageRecord(6, 10, 5, 2, 500, 500, 4, 4)}, 5},
		{"intermediate-parent", []guardRecord{base[0], base[1], base[2], managedLineageRecord(4, 9, 5, 2, 500, 500, 2, 2), managedLineageRecord(5, 10, 5, 2, 500, 500, 3, 3)}, 5},
		{"unknown-inode", []guardRecord{base[0], base[1], base[2], managedLineageRecord(4, 10, 2, 1, 200, 200, 99, 3)}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLineage()
			for _, r := range tc.records {
				if err := l.observe(r); err != nil {
					t.Fatalf("observe: %v", err)
				}
			}
			if !l.nodes[tc.target].tainted {
				t.Fatal("invalid transition was not tainted")
			}
			if _, err := l.ancestry(tc.target, "wrapper"); err == nil {
				t.Fatal("tainted ancestry accepted")
			}
		})
	}
	t.Run("worker-parent", func(t *testing.T) {
		l := newLineage()
		records := []guardRecord{base[0], base[1], managedLineageRecord(3, 9, 8, 1, 100, 101, 2, 2), managedLineageRecord(4, 9, 2, 8, 200, 200, 2, 2), managedLineageRecord(5, 10, 2, 8, 200, 200, 3, 3), managedLineageRecord(6, 10, 2, 8, 200, 200, 4, 4)}
		for _, r := range records {
			if err := l.observe(r); err != nil {
				t.Fatal(err)
			}
		}
		if l.nodes[2].tainted || !l.nodes[2].managedContainerd {
			t.Fatal("worker-parent rejected")
		}
	})
	t.Run("features", func(t *testing.T) {
		l := newLineage()
		for _, r := range []guardRecord{base[0], base[1], base[2], managedLineageRecord(4, 10, 2, 1, 200, 200, 6, 3)} {
			if err := l.observe(r); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := l.ancestry(2, "wrapper"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("generation", func(t *testing.T) {
		l := newLineage()
		for _, r := range base[:3] {
			if err := l.observe(r); err != nil {
				t.Fatal(err)
			}
		}
		if err := l.observe(managedLineageRecord(4, 10, 2, 1, 200, 200, 3, 4)); err == nil {
			t.Fatal("invalid generation accepted")
		}
	})
	t.Run("thread-inherits-launcher", func(t *testing.T) {
		l := newLineage()
		for _, r := range []guardRecord{base[0], base[1], base[2], base[3], managedLineageRecord(5, 9, 5, 2, 200, 201, 3, 3), managedLineageRecord(6, 10, 5, 2, 200, 201, 4, 4)} {
			if err := l.observe(r); err != nil {
				t.Fatal(err)
			}
		}
		if !l.nodes[5].tainted {
			t.Fatal("thread borrowed launcher exec")
		}
	})
	t.Run("prior-taint-persists", func(t *testing.T) {
		l := newLineage()
		for _, r := range []guardRecord{base[0], base[1], base[2], managedLineageRecord(4, 10, 2, 1, 200, 200, 99, 3), managedLineageRecord(5, 10, 2, 1, 200, 200, 3, 4)} {
			if err := l.observe(r); err != nil {
				t.Fatal(err)
			}
		}
		if !l.nodes[2].tainted {
			t.Fatal("taint was cleared")
		}
	})
	t.Run("other-task-history", func(t *testing.T) {
		l := newLineage()
		for _, r := range []guardRecord{base[0], base[1], base[2], base[3], managedLineageRecord(5, 9, 5, 1, 500, 500, 2, 2), managedLineageRecord(6, 10, 5, 1, 500, 500, 4, 3)} {
			if err := l.observe(r); err != nil {
				t.Fatal(err)
			}
		}
		if !l.nodes[5].tainted {
			t.Fatal("another task borrowed launcher history")
		}
	})
}
