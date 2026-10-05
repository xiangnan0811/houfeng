package hostsample_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"houfeng/agent/hostsample"
	"houfeng/internal/contracts/agentapi"
)

func TestCollectCPUUsesEachBasicCounterOnceWithOneDenominator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		values []uint64
		extra  []uint64
		want   float64
	}{
		{
			name:   "guest",
			values: []uint64{50, 0, 0, 50, 0, 0, 0, 0},
			extra:  []uint64{50, 0},
			want:   50,
		},
		{
			name:   "guest nice",
			values: []uint64{0, 50, 0, 50, 0, 0, 0, 0},
			extra:  []uint64{0, 50},
			want:   50,
		},
		{
			name:   "guest and guest nice",
			values: []uint64{50, 50, 0, 100, 0, 0, 0, 0},
			extra:  []uint64{50, 50},
			want:   50,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			provider := newCPUProvider([]string{
				cpuStatWithExtras(make([]uint64, 8), 0, 0),
				cpuStatWithExtras(tt.values, tt.extra...),
			}, nil)
			firstAt := collectorTestTime()
			if _, err := provider.Collect(firstAt); err != nil {
				t.Fatalf("first Collect() error = %v", err)
			}
			second, err := provider.Collect(firstAt.Add(10 * time.Second))
			if err != nil {
				t.Fatalf("second Collect() error = %v", err)
			}
			assertCPURates(t, second, tt.want, 0, 0, true)
		})
	}
}

func TestCollectCPUUsesIdleIowaitAndStealWithKnownValues(t *testing.T) {
	t.Parallel()

	provider := newCPUProvider([]string{
		cpuStat(0, 0, 0, 0, 0, 0, 0, 0),
		cpuStat(50, 0, 0, 100, 25, 0, 0, 25),
	}, nil)
	firstAt := collectorTestTime()
	if _, err := provider.Collect(firstAt); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	assertCPURates(t, second, 37.5, 12.5, 12.5, true)
}

func TestCollectCPUAcceptsLegacySevenCounterStatLine(t *testing.T) {
	t.Parallel()

	provider := newCPUProvider([]string{
		cpuStat(100, 0, 0, 100, 0, 0, 0),
		cpuStat(150, 0, 0, 100, 0, 0, 0),
	}, nil)
	firstAt := collectorTestTime()
	if _, err := provider.Collect(firstAt); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	assertCPURates(t, second, 100, 0, 0, true)
}

func TestCollectCPUAllBusyAndTrueZeroAreValid(t *testing.T) {
	t.Parallel()

	provider := newCPUProvider([]string{
		cpuStat(0, 0, 0, 0, 0, 0, 0, 0),
		cpuStat(100, 0, 0, 0, 0, 0, 0, 0),
		cpuStat(100, 0, 0, 100, 0, 0, 0, 0),
	}, nil)
	firstAt := collectorTestTime()
	if _, err := provider.Collect(firstAt); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	busy, err := provider.Collect(firstAt.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("busy Collect() error = %v", err)
	}
	assertCPURates(t, busy, 100, 0, 0, true)

	zero, err := provider.Collect(firstAt.Add(20 * time.Second))
	if err != nil {
		t.Fatalf("zero Collect() error = %v", err)
	}
	assertCPURates(t, zero, 0, 0, 0, true)
}

func TestCollectIowaitRollbackMarksOnlyCPUInvalidAndNextIntervalRecovers(t *testing.T) {
	t.Parallel()

	provider := newCPUProvider([]string{
		cpuStat(100, 0, 100, 700, 100, 0, 0, 0),
		cpuStat(140, 0, 140, 730, 90, 10, 0, 0),
		cpuStat(180, 0, 180, 760, 100, 20, 0, 0),
	}, nil)
	firstAt := collectorTestTime()
	for index, observedAt := range []time.Time{
		firstAt,
		firstAt.Add(10 * time.Second),
		firstAt.Add(20 * time.Second),
	} {
		sample, err := provider.Collect(observedAt)
		if err != nil {
			t.Fatalf("Collect(%d) error = %v", index, err)
		}
		assertUnrelatedHostMetrics(t, sample)
		wantValid := index == 2
		if sample.CPURatesValid == nil || *sample.CPURatesValid != wantValid {
			t.Fatalf("Collect(%d) CPURatesValid = %v, want %t", index, sample.CPURatesValid, wantValid)
		}
		if index == 1 && (sample.CPUUsagePct != 0 || sample.CPUIOWaitPct != 0 || sample.CPUStealPct != 0) {
			t.Fatalf("rollback CPU values = %v/%v/%v, want zero placeholders", sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct)
		}
	}
}

func TestCollectMarksInvalidCPUWithoutDroppingHostSample(t *testing.T) {
	t.Parallel()

	base := []uint64{100, 100, 100, 100, 100, 100, 100, 100}
	cases := []struct {
		name       string
		stats      []string
		statErrors map[int]error
		times      []time.Time
	}{
		{
			name:  "first sample",
			stats: []string{cpuStat(base...)},
			times: []time.Time{collectorTestTime()},
		},
		{
			name:  "zero delta",
			stats: []string{cpuStat(base...), cpuStat(base...)},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(10 * time.Second)},
		},
		{
			name:  "observed at zero",
			stats: []string{cpuStat(base...), cpuStat(addCPUValues(base, 1)...)},
			times: []time.Time{{}, collectorTestTime()},
		},
		{
			name:  "observed at does not increase",
			stats: []string{cpuStat(base...), cpuStat(addCPUValues(base, 1)...)},
			times: []time.Time{collectorTestTime(), collectorTestTime()},
		},
		{
			name:  "observed at moves backwards",
			stats: []string{cpuStat(base...), cpuStat(addCPUValues(base, 1)...)},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(-time.Second)},
		},
		{
			name:  "fields change",
			stats: []string{cpuStat(base[:7]...), cpuStat(addCPUValues(base, 1)...)},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(10 * time.Second)},
		},
		{
			name:  "total reset",
			stats: []string{cpuStat(base...), cpuStat(1, 1, 1, 1, 1, 1, 1, 1)},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(10 * time.Second)},
		},
		{
			name:  "cpu file missing",
			stats: []string{cpuStat(base...), cpuStat(addCPUValues(base, 1)...)},
			statErrors: map[int]error{
				1: errors.New("stat unavailable"),
			},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(10 * time.Second)},
		},
		{
			name:  "cpu line invalid",
			stats: []string{cpuStat(base...), "cpu 101 101 invalid 101 101 101 101 101\n"},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(10 * time.Second)},
		},
		{
			name: "counter sum overflow",
			stats: []string{
				cpuStat(math.MaxUint64, 1, 0, 0, 0, 0, 0, 0),
			},
			times: []time.Time{collectorTestTime()},
		},
	}
	for index := range base {
		current := addCPUValues(base, 10)
		current[index] = base[index] - 1
		cases = append(cases, struct {
			name       string
			stats      []string
			statErrors map[int]error
			times      []time.Time
		}{
			name:  fmt.Sprintf("counter %d rolls back while total grows", index),
			stats: []string{cpuStat(base...), cpuStat(current...)},
			times: []time.Time{collectorTestTime(), collectorTestTime().Add(10 * time.Second)},
		})
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			provider := newCPUProvider(tt.stats, tt.statErrors)
			for index, observedAt := range tt.times {
				sample, err := provider.Collect(observedAt)
				if err != nil {
					t.Fatalf("Collect(%d) error = %v", index, err)
				}
				if sample.CPURatesValid == nil || *sample.CPURatesValid {
					t.Fatalf("Collect(%d) CPURatesValid = %v, want false", index, sample.CPURatesValid)
				}
				if sample.CPUUsagePct != 0 || sample.CPUIOWaitPct != 0 || sample.CPUStealPct != 0 {
					t.Fatalf("Collect(%d) invalid CPU values = %v/%v/%v, want zero placeholders", index, sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct)
				}
				assertUnrelatedHostMetrics(t, sample)
			}
		})
	}
}

func TestCollectCPURecoversOnlyAfterRebuildingBaseline(t *testing.T) {
	t.Parallel()

	base := []uint64{100, 100, 100, 100, 100, 100, 100, 100}
	next := addCPUValues(base, 10)
	final := addCPUValues(next, 10)
	provider := newCPUProvider(
		[]string{cpuStat(base...), "", cpuStat(next...), cpuStat(final...)},
		map[int]error{1: errors.New("stat unavailable")},
	)
	firstAt := collectorTestTime()
	for index, observedAt := range []time.Time{
		firstAt,
		firstAt.Add(10 * time.Second),
		firstAt.Add(20 * time.Second),
		firstAt.Add(30 * time.Second),
	} {
		sample, err := provider.Collect(observedAt)
		if err != nil {
			t.Fatalf("Collect(%d) error = %v", index, err)
		}
		wantValid := index == 3
		if sample.CPURatesValid == nil || *sample.CPURatesValid != wantValid {
			t.Fatalf("Collect(%d) CPURatesValid = %v, want %t", index, sample.CPURatesValid, wantValid)
		}
		assertUnrelatedHostMetrics(t, sample)
	}
}

func TestCollectCPUOverflowRecoveryAlsoRebuildsBaseline(t *testing.T) {
	t.Parallel()

	base := []uint64{100, 100, 100, 100, 100, 100, 100, 100}
	next := addCPUValues(base, 10)
	provider := newCPUProvider([]string{
		cpuStat(math.MaxUint64, 1, 0, 0, 0, 0, 0, 0),
		cpuStat(base...),
		cpuStat(next...),
	}, nil)
	firstAt := collectorTestTime()
	for index, observedAt := range []time.Time{
		firstAt,
		firstAt.Add(10 * time.Second),
		firstAt.Add(20 * time.Second),
	} {
		sample, err := provider.Collect(observedAt)
		if err != nil {
			t.Fatalf("Collect(%d) error = %v", index, err)
		}
		wantValid := index == 2
		if sample.CPURatesValid == nil || *sample.CPURatesValid != wantValid {
			t.Fatalf("Collect(%d) CPURatesValid = %v, want %t", index, sample.CPURatesValid, wantValid)
		}
	}
}

func assertCPURates(t *testing.T, sample agentapi.HostSamplePayload, usage, iowait, steal float64, valid bool) {
	t.Helper()
	if sample.CPURatesValid == nil || *sample.CPURatesValid != valid {
		t.Fatalf("CPURatesValid = %v, want %t", sample.CPURatesValid, valid)
	}
	if math.Abs(sample.CPUUsagePct-usage) > 1e-12 || math.Abs(sample.CPUIOWaitPct-iowait) > 1e-12 || math.Abs(sample.CPUStealPct-steal) > 1e-12 {
		t.Fatalf("CPU rates = %v/%v/%v, want %v/%v/%v", sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct, usage, iowait, steal)
	}
}

func assertUnrelatedHostMetrics(t *testing.T, sample agentapi.HostSamplePayload) {
	t.Helper()
	if sample.Load1 != 1.25 || sample.MemUsedPct != 50 || sample.MemAvailableBytes != 500*1024 || sample.DiskUsedPct != 50 {
		t.Fatalf("unrelated host metrics = %v/%v/%d/%v, want 1.25/50/%d/50", sample.Load1, sample.MemUsedPct, sample.MemAvailableBytes, sample.DiskUsedPct, 500*1024)
	}
}

func addCPUValues(values []uint64, amount uint64) []uint64 {
	out := append([]uint64(nil), values...)
	for index := range out {
		out[index] += amount
	}
	return out
}

func cpuStat(values ...uint64) string {
	return cpuStatWithExtras(values)
}

func cpuStatWithExtras(values []uint64, extras ...uint64) string {
	fields := make([]string, 0, 1+len(values)+len(extras))
	fields = append(fields, "cpu")
	for _, value := range values {
		fields = append(fields, fmt.Sprintf("%d", value))
	}
	for _, value := range extras {
		fields = append(fields, fmt.Sprintf("%d", value))
	}
	return strings.Join(fields, " ") + "\n"
}

func newCPUProvider(stats []string, statErrors map[int]error) *hostsample.Provider {
	files := map[string]string{
		"/proc/loadavg":   "1.25 0.75 0.50 1/100 123\n",
		"/proc/meminfo":   "MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 500 kB\n",
		"/proc/uptime":    "3600.00 0.00\n",
		"/proc/net/dev":   "eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
		"/proc/diskstats": "8 0 sda 0 0 100 0 0 0 200 0 0 50 0\n",
	}
	calls := make(map[string]int)
	return hostsample.NewWithDeps(func(path string) ([]byte, error) {
		index := calls[path]
		calls[path] = index + 1
		if path == "/proc/stat" {
			if err, ok := statErrors[index]; ok && err != nil {
				return nil, err
			}
			if index >= len(stats) {
				index = len(stats) - 1
			}
			return []byte(stats[index]), nil
		}
		value, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("unexpected read path %q", path)
		}
		return []byte(value), nil
	}, func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 500, Bsize: 4096, Files: 100, Ffree: 50}, nil
	})
}

func collectorTestTime() time.Time {
	return time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
}
