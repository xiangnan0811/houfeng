package hostsample_test

import (
	"testing"
	"time"

	"houfeng/agent/hostsample"
)

func TestCollectNetworkRatesValidityDistinguishesZeroAndCounterReset(t *testing.T) {
	provider := hostsample.NewWithDeps(sequencedReadFile(map[string][]string{
		"/proc/loadavg":   {"0.10 0.20 0.30 1/100 123\n"},
		"/proc/meminfo":   {"MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n"},
		"/proc/uptime":    {"10.00 0.00\n", "20.00 0.00\n", "30.00 0.00\n"},
		"/proc/stat":      {"cpu  100 0 50 800 20 0 0 10 0 0\n"},
		"/proc/net/dev":   {"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n", "eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n", "eth0: 900 0 0 0 0 0 0 0 400 0 0 0 0 0 0 0\n"},
		"/proc/diskstats": {"8 0 sda 0 0 100 0 0 0 200 0 0 100 0\n"},
	}), func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 500, Bsize: 4096, Files: 100, Ffree: 50}, nil
	})

	firstAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	first, err := provider.Collect(firstAt)
	if err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	third, err := provider.Collect(firstAt.Add(20 * time.Second))
	if err != nil {
		t.Fatalf("third Collect() error = %v", err)
	}

	if first.NetworkRatesValid == nil || *first.NetworkRatesValid {
		t.Fatalf("first NetworkRatesValid = %v, want explicit false", first.NetworkRatesValid)
	}
	if second.NetworkRatesValid == nil || !*second.NetworkRatesValid {
		t.Fatalf("second NetworkRatesValid = %v, want true for established zero", second.NetworkRatesValid)
	}
	if second.NetInBytesPerSec != 0 || second.NetOutBytesPerSec != 0 {
		t.Fatalf("second zero rates = %d/%d, want zero", second.NetInBytesPerSec, second.NetOutBytesPerSec)
	}
	if third.NetworkRatesValid == nil || *third.NetworkRatesValid {
		t.Fatalf("third NetworkRatesValid = %v, want explicit false after counter reset", third.NetworkRatesValid)
	}
	if third.NetInBytesPerSec != 0 || third.NetOutBytesPerSec != 0 {
		t.Fatalf("third reset rates = %d/%d, want zero payload values", third.NetInBytesPerSec, third.NetOutBytesPerSec)
	}
}

func TestCollectNetworkRatesRejectsMaskedInterfaceReset(t *testing.T) {
	provider := hostsample.NewWithDeps(sequencedReadFile(map[string][]string{
		"/proc/loadavg": {"0.10 0.20 0.30 1/100 123\n"},
		"/proc/meminfo": {"MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n"},
		"/proc/uptime":  {"10.00 0.00\n", "20.00 0.00\n", "30.00 0.00\n"},
		"/proc/stat":    {"cpu  100 0 50 800 20 0 0 10 0 0\n"},
		"/proc/net/dev": {
			"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\nwlan0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
			"eth0: 900 0 0 0 0 0 0 0 400 0 0 0 0 0 0 0\nwlan0: 2200 0 0 0 0 0 0 0 1700 0 0 0 0 0 0 0\n",
			"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\nwlan0: 2400 0 0 0 0 0 0 0 1900 0 0 0 0 0 0 0\n",
		},
		"/proc/diskstats": {"8 0 sda 0 0 100 0 0 0 200 0 0 100 0\n"},
	}), func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 500, Bsize: 4096, Files: 100, Ffree: 50}, nil
	})

	firstAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	first, err := provider.Collect(firstAt)
	if err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	third, err := provider.Collect(firstAt.Add(20 * time.Second))
	if err != nil {
		t.Fatalf("third Collect() error = %v", err)
	}

	if first.NetworkRatesValid == nil || *first.NetworkRatesValid {
		t.Fatalf("first NetworkRatesValid = %v, want explicit false", first.NetworkRatesValid)
	}
	if second.NetworkRatesValid == nil || *second.NetworkRatesValid {
		t.Fatalf("masked reset NetworkRatesValid = %v, want explicit false", second.NetworkRatesValid)
	}
	if third.NetworkRatesValid == nil || !*third.NetworkRatesValid {
		t.Fatalf("post-reset NetworkRatesValid = %v, want true for new comparable baseline", third.NetworkRatesValid)
	}
	if third.NetInBytesPerSec != 30 || third.NetOutBytesPerSec != 30 {
		t.Fatalf("post-reset rates = %d/%d, want 30/30", third.NetInBytesPerSec, third.NetOutBytesPerSec)
	}
}

func TestCollectNetworkRatesRejectsInterfaceSetChanges(t *testing.T) {
	provider := hostsample.NewWithDeps(sequencedReadFile(map[string][]string{
		"/proc/loadavg": {"0.10 0.20 0.30 1/100 123\n"},
		"/proc/meminfo": {"MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n"},
		"/proc/uptime":  {"10.00 0.00\n", "20.00 0.00\n", "30.00 0.00\n", "40.00 0.00\n", "50.00 0.00\n"},
		"/proc/stat":    {"cpu  100 0 50 800 20 0 0 10 0 0\n"},
		"/proc/net/dev": {
			"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
			"eth0: 2000 0 0 0 0 0 0 0 1500 0 0 0 0 0 0 0\nwlan0: 500 0 0 0 0 0 0 0 250 0 0 0 0 0 0 0\n",
			"eth0: 3000 0 0 0 0 0 0 0 1700 0 0 0 0 0 0 0\nwlan0: 700 0 0 0 0 0 0 0 450 0 0 0 0 0 0 0\n",
			"eth0: 4000 0 0 0 0 0 0 0 1900 0 0 0 0 0 0 0\n",
			"eth0: 5000 0 0 0 0 0 0 0 2100 0 0 0 0 0 0 0\n",
		},
		"/proc/diskstats": {"8 0 sda 0 0 100 0 0 0 200 0 0 100 0\n"},
	}), func(string) (hostsample.FilesystemStats, error) {
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
	third, err := provider.Collect(firstAt.Add(20 * time.Second))
	if err != nil {
		t.Fatalf("third Collect() error = %v", err)
	}
	fourth, err := provider.Collect(firstAt.Add(30 * time.Second))
	if err != nil {
		t.Fatalf("fourth Collect() error = %v", err)
	}
	fifth, err := provider.Collect(firstAt.Add(40 * time.Second))
	if err != nil {
		t.Fatalf("fifth Collect() error = %v", err)
	}

	if second.NetworkRatesValid == nil || *second.NetworkRatesValid {
		t.Fatalf("interface add NetworkRatesValid = %v, want explicit false", second.NetworkRatesValid)
	}
	if third.NetworkRatesValid == nil || !*third.NetworkRatesValid {
		t.Fatalf("stable two-interface NetworkRatesValid = %v, want true", third.NetworkRatesValid)
	}
	if third.NetInBytesPerSec != 120 || third.NetOutBytesPerSec != 40 {
		t.Fatalf("stable two-interface rates = %d/%d, want 120/40", third.NetInBytesPerSec, third.NetOutBytesPerSec)
	}
	if fourth.NetworkRatesValid == nil || *fourth.NetworkRatesValid {
		t.Fatalf("interface removal NetworkRatesValid = %v, want explicit false", fourth.NetworkRatesValid)
	}
	if fifth.NetworkRatesValid == nil || !*fifth.NetworkRatesValid {
		t.Fatalf("stable one-interface NetworkRatesValid = %v, want true", fifth.NetworkRatesValid)
	}
	if fifth.NetInBytesPerSec != 100 || fifth.NetOutBytesPerSec != 20 {
		t.Fatalf("stable one-interface rates = %d/%d, want 100/20", fifth.NetInBytesPerSec, fifth.NetOutBytesPerSec)
	}

}

func TestCollectNetworkRatesInvalidForNonPositiveInterval(t *testing.T) {
	provider := hostsample.NewWithDeps(sequencedReadFile(map[string][]string{
		"/proc/loadavg":   {"0.10 0.20 0.30 1/100 123\n"},
		"/proc/meminfo":   {"MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 1000 kB\n"},
		"/proc/uptime":    {"10.00 0.00\n"},
		"/proc/stat":      {"cpu  100 0 50 800 20 0 0 10 0 0\n"},
		"/proc/net/dev":   {"eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n", "eth0: 2000 0 0 0 0 0 0 0 1500 0 0 0 0 0 0 0\n"},
		"/proc/diskstats": {"8 0 sda 0 0 100 0 0 0 200 0 0 100 0\n"},
	}), func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 500, Bsize: 4096, Files: 100, Ffree: 50}, nil
	})

	observedAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	if _, err := provider.Collect(observedAt); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(observedAt)
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if second.NetworkRatesValid == nil || *second.NetworkRatesValid {
		t.Fatalf("NetworkRatesValid = %v, want explicit false", second.NetworkRatesValid)
	}
	if second.NetInBytesPerSec != 0 || second.NetOutBytesPerSec != 0 {
		t.Fatalf("non-positive interval rates = %d/%d, want zero payload values", second.NetInBytesPerSec, second.NetOutBytesPerSec)
	}
}
