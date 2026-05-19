package hostsample_test

import (
	"testing"
	"time"

	"houfeng/agent/hostsample"
)

func TestCollectReturnsCurrentMetricsAndFirstSampleZeroRates(t *testing.T) {
	provider := hostsample.NewWithDeps(fakeReadFile(map[string]string{
		"/proc/loadavg":   "1.25 0.75 0.50 1/100 123\n",
		"/proc/meminfo":   "MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 500 kB\nSwapFree: 400 kB\n",
		"/proc/uptime":    "3600.00 0.00\n",
		"/proc/stat":      "cpu  100 0 50 800 20 0 0 10 0 0\n",
		"/proc/net/dev":   "Inter-|\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\nlo: 10 0 0 0 0 0 0 0 10 0 0 0 0 0 0 0\n",
		"/proc/diskstats": "8 0 sda 0 0 100 0 0 0 200 0 0 50 0\n",
	}), func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 200, Bsize: 4096, Files: 100, Ffree: 20}, nil
	})

	sample, err := provider.Collect(time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if sample.Load1 != 1.25 || sample.Load5 != 0.75 || sample.Load15 != 0.50 {
		t.Fatalf("unexpected load averages: %#v", sample)
	}
	if sample.MemUsedPct != 75 {
		t.Fatalf("MemUsedPct = %v, want 75", sample.MemUsedPct)
	}
	if sample.MemAvailableBytes != 250*1024 {
		t.Fatalf("MemAvailableBytes = %d, want %d", sample.MemAvailableBytes, 250*1024)
	}
	if sample.MemTotalBytes != 1000*1024 {
		t.Fatalf("MemTotalBytes = %d, want %d", sample.MemTotalBytes, 1000*1024)
	}
	if sample.SwapUsedPct != 20 {
		t.Fatalf("SwapUsedPct = %v, want 20", sample.SwapUsedPct)
	}
	if sample.DiskUsedPct != 80 {
		t.Fatalf("DiskUsedPct = %v, want 80", sample.DiskUsedPct)
	}
	if sample.DiskTotalBytes != 1000*4096 {
		t.Fatalf("DiskTotalBytes = %d, want %d", sample.DiskTotalBytes, 1000*4096)
	}
	if sample.InodeUsedPct != 80 {
		t.Fatalf("InodeUsedPct = %v, want 80", sample.InodeUsedPct)
	}
	if sample.UptimeSeconds != 3600 {
		t.Fatalf("UptimeSeconds = %d, want 3600", sample.UptimeSeconds)
	}
	if sample.CPUUsagePct != 0 || sample.NetInBytesPerSec != 0 || sample.DiskReadBytesPerSec != 0 {
		t.Fatalf("first sample should start with zero rate-based fields: %#v", sample)
	}
}

func TestCollectComputesRateBasedFieldsFromPreviousSnapshot(t *testing.T) {
	files := map[string][]string{
		"/proc/loadavg":   {"0.10 0.20 0.30 1/100 123\n", "0.20 0.30 0.40 1/100 124\n"},
		"/proc/meminfo":   {"MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n", "MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 1000 kB\nSwapFree: 900 kB\n"},
		"/proc/uptime":    {"10.00 0.00\n", "20.00 0.00\n"},
		"/proc/stat":      {"cpu  100 0 50 800 20 0 0 10 0 0\n", "cpu  180 0 90 860 30 0 0 20 0 0\n"},
		"/proc/net/dev":   {"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n", "eth0: 3000 0 0 0 0 0 0 0 2500 0 0 0 0 0 0 0\n"},
		"/proc/diskstats": {"8 0 sda 0 0 100 0 0 0 200 0 0 100 0\n", "8 0 sda 0 0 300 0 0 0 500 0 0 300 0\n"},
	}
	provider := hostsample.NewWithDeps(sequencedReadFile(files), func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 500, Bsize: 4096, Files: 100, Ffree: 50}, nil
	})

	firstAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	if _, err := provider.Collect(firstAt); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if second.CPUUsagePct <= 0 {
		t.Fatalf("CPUUsagePct = %v, want > 0", second.CPUUsagePct)
	}
	if second.CPUIOWaitPct <= 0 {
		t.Fatalf("CPUIOWaitPct = %v, want > 0", second.CPUIOWaitPct)
	}
	if second.CPUStealPct <= 0 {
		t.Fatalf("CPUStealPct = %v, want > 0", second.CPUStealPct)
	}
	if second.NetInBytesPerSec != 200 {
		t.Fatalf("NetInBytesPerSec = %d, want 200", second.NetInBytesPerSec)
	}
	if second.NetOutBytesPerSec != 200 {
		t.Fatalf("NetOutBytesPerSec = %d, want 200", second.NetOutBytesPerSec)
	}
	if second.DiskReadBytesPerSec <= 0 || second.DiskWriteBytesPerSec <= 0 {
		t.Fatalf("disk rates should be > 0: %#v", second)
	}
	if second.DiskBusyPct <= 0 {
		t.Fatalf("DiskBusyPct = %v, want > 0", second.DiskBusyPct)
	}
}

func TestCollectIgnoresPartitionRowsInDiskStats(t *testing.T) {
	files := map[string][]string{
		"/proc/loadavg": {"0.10 0.20 0.30 1/100 123\n", "0.10 0.20 0.30 1/100 124\n"},
		"/proc/meminfo": {"MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n", "MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n"},
		"/proc/uptime":  {"10.00 0.00\n", "20.00 0.00\n"},
		"/proc/stat":    {"cpu  100 0 50 800 20 0 0 10 0 0\n", "cpu  180 0 90 860 30 0 0 20 0 0\n"},
		"/proc/net/dev": {"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n", "eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n"},
		"/proc/diskstats": {
			"8 0 sda 0 0 100 0 0 0 200 0 0 100 0\n8 1 sda1 0 0 1000 0 0 0 2000 0 0 900 0\n259 0 nvme0n1 0 0 50 0 0 0 80 0 0 40 0\n259 1 nvme0n1p1 0 0 500 0 0 0 800 0 0 400 0\n",
			"8 0 sda 0 0 300 0 0 0 500 0 0 300 0\n8 1 sda1 0 0 5000 0 0 0 7000 0 0 4900 0\n259 0 nvme0n1 0 0 150 0 0 0 180 0 0 140 0\n259 1 nvme0n1p1 0 0 7000 0 0 0 9000 0 0 6400 0\n",
		},
	}
	provider := hostsample.NewWithDeps(sequencedReadFile(files), func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 500, Bsize: 4096, Files: 100, Ffree: 50}, nil
	})

	firstAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	if _, err := provider.Collect(firstAt); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}

	if second.DiskReadBytesPerSec != 15360 {
		t.Fatalf("DiskReadBytesPerSec = %d, want %d", second.DiskReadBytesPerSec, 15360)
	}
	if second.DiskWriteBytesPerSec != 20480 {
		t.Fatalf("DiskWriteBytesPerSec = %d, want %d", second.DiskWriteBytesPerSec, 20480)
	}
	if second.DiskBusyPct != 3 {
		t.Fatalf("DiskBusyPct = %v, want %v", second.DiskBusyPct, 3.0)
	}
}

func fakeReadFile(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) { return []byte(files[path]), nil }
}

func sequencedReadFile(files map[string][]string) func(string) ([]byte, error) {
	index := map[string]int{}
	return func(path string) ([]byte, error) {
		values := files[path]
		i := index[path]
		if i >= len(values) {
			i = len(values) - 1
		}
		index[path] = index[path] + 1
		return []byte(values[i]), nil
	}
}
