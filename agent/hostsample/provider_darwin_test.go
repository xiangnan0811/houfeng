package hostsample

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCollectDarwinReturnsCoreMetricsWithoutProcFS(t *testing.T) {
	commands := map[string]string{
		"sysctl -n vm.loadavg":    "{ 1.25 0.75 0.50 }\n",
		"sysctl -n hw.memsize":    "4096000\n",
		"sysctl -n vm.swapusage":  "total = 100.00M  used = 25.00M  free = 75.00M  (encrypted)\n",
		"sysctl -n kern.boottime": "{ sec = 1776816354, usec = 120451 } Wed Apr 22 08:05:54 2026\n",
		"vm_stat":                 "Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free: 100.\nPages inactive: 200.\nPages speculative: 50.\n",
	}
	provider := newWithPlatformDeps(
		"darwin",
		func(path string) ([]byte, error) {
			if strings.HasPrefix(path, "/proc/") {
				t.Fatalf("darwin collector read procfs path %q", path)
			}
			return nil, fmt.Errorf("unexpected file read %q", path)
		},
		func(string) (FilesystemStats, error) {
			return FilesystemStats{Blocks: 1000, Bfree: 200, Files: 100, Ffree: 20}, nil
		},
		func(name string, args ...string) ([]byte, error) {
			key := strings.TrimSpace(name + " " + strings.Join(args, " "))
			value, ok := commands[key]
			if !ok {
				return nil, fmt.Errorf("unexpected command %q", key)
			}
			return []byte(value), nil
		},
	)

	sample, err := provider.Collect(time.Unix(1776819954, 0))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if sample.Load1 != 1.25 || sample.Load5 != 0.75 || sample.Load15 != 0.50 {
		t.Fatalf("unexpected load averages: %#v", sample)
	}
	if sample.MemAvailableBytes != 350*4096 {
		t.Fatalf("MemAvailableBytes = %d, want %d", sample.MemAvailableBytes, 350*4096)
	}
	if sample.MemUsedPct != 65 {
		t.Fatalf("MemUsedPct = %v, want 65", sample.MemUsedPct)
	}
	if sample.SwapUsedPct != 25 {
		t.Fatalf("SwapUsedPct = %v, want 25", sample.SwapUsedPct)
	}
	if sample.DiskUsedPct != 80 {
		t.Fatalf("DiskUsedPct = %v, want 80", sample.DiskUsedPct)
	}
	if sample.InodeUsedPct != 80 {
		t.Fatalf("InodeUsedPct = %v, want 80", sample.InodeUsedPct)
	}
	if sample.UptimeSeconds != 3600 {
		t.Fatalf("UptimeSeconds = %d, want 3600", sample.UptimeSeconds)
	}
	if sample.CPUUsagePct != 0 || sample.NetInBytesPerSec != 0 || sample.DiskReadBytesPerSec != 0 {
		t.Fatalf("darwin rate-based fields should start at zero: %#v", sample)
	}
}
