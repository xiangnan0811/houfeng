package hostsample

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type FilesystemStats struct {
	Blocks uint64
	Bfree  uint64
	Files  uint64
	Ffree  uint64
}

type snapshot struct {
	observedAt time.Time
	cpuTotal   uint64
	cpuIdle    uint64
	cpuIowait  uint64
	cpuSteal   uint64
	netIn      uint64
	netOut     uint64
	diskRead   uint64
	diskWrite  uint64
	diskBusyMS uint64
}

type Provider struct {
	readFile func(string) ([]byte, error)
	statFS   func(string) (FilesystemStats, error)
	previous *snapshot
}

func New() *Provider {
	return NewWithDeps(os.ReadFile, defaultStatFS)
}

func NewWithDeps(readFile func(string) ([]byte, error), statFS func(string) (FilesystemStats, error)) *Provider {
	if readFile == nil {
		readFile = os.ReadFile
	}
	if statFS == nil {
		statFS = defaultStatFS
	}
	return &Provider{readFile: readFile, statFS: statFS}
}

func (p *Provider) Collect(observedAt time.Time) (agentapi.HostSamplePayload, error) {
	loadavgRaw, err := p.readFile("/proc/loadavg")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("read /proc/loadavg: %w", err)
	}
	load1, load5, load15, err := parseLoadAvg(loadavgRaw)
	if err != nil {
		return agentapi.HostSamplePayload{}, err
	}

	meminfoRaw, err := p.readFile("/proc/meminfo")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	memUsedPct, memAvailableBytes, swapUsedPct, err := parseMemInfo(meminfoRaw)
	if err != nil {
		return agentapi.HostSamplePayload{}, err
	}

	uptimeRaw, err := p.readFile("/proc/uptime")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("read /proc/uptime: %w", err)
	}
	uptimeSeconds, err := parseUptime(uptimeRaw)
	if err != nil {
		return agentapi.HostSamplePayload{}, err
	}

	statRaw, err := p.readFile("/proc/stat")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("read /proc/stat: %w", err)
	}
	cpuTotal, cpuIdle, cpuIowait, cpuSteal, err := parseCPUStat(statRaw)
	if err != nil {
		return agentapi.HostSamplePayload{}, err
	}

	netRaw, err := p.readFile("/proc/net/dev")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("read /proc/net/dev: %w", err)
	}
	netIn, netOut, err := parseNetDev(netRaw)
	if err != nil {
		return agentapi.HostSamplePayload{}, err
	}

	diskRaw, err := p.readFile("/proc/diskstats")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("read /proc/diskstats: %w", err)
	}
	diskRead, diskWrite, diskBusyMS, err := parseDiskStats(diskRaw)
	if err != nil {
		return agentapi.HostSamplePayload{}, err
	}

	fsStats, err := p.statFS("/")
	if err != nil {
		return agentapi.HostSamplePayload{}, fmt.Errorf("statfs /: %w", err)
	}
	diskUsedPct, inodeUsedPct := deriveUsagePercents(fsStats)

	sample := agentapi.HostSamplePayload{
		ObservedAt:        observedAt,
		Load1:             load1,
		Load5:             load5,
		Load15:            load15,
		MemUsedPct:        memUsedPct,
		MemAvailableBytes: memAvailableBytes,
		SwapUsedPct:       swapUsedPct,
		DiskUsedPct:       diskUsedPct,
		InodeUsedPct:      inodeUsedPct,
		UptimeSeconds:     uptimeSeconds,
	}

	current := &snapshot{
		observedAt: observedAt,
		cpuTotal:   cpuTotal,
		cpuIdle:    cpuIdle,
		cpuIowait:  cpuIowait,
		cpuSteal:   cpuSteal,
		netIn:      netIn,
		netOut:     netOut,
		diskRead:   diskRead,
		diskWrite:  diskWrite,
		diskBusyMS: diskBusyMS,
	}
	if p.previous != nil {
		elapsedSeconds := observedAt.Sub(p.previous.observedAt).Seconds()
		if elapsedSeconds > 0 {
			sample.CPUUsagePct = cpuUsagePct(*p.previous, *current)
			sample.CPUIOWaitPct = cpuIowaitPct(*p.previous, *current)
			sample.CPUStealPct = cpuStealPct(*p.previous, *current)
			sample.NetInBytesPerSec = rateBytesPerSecond(p.previous.netIn, current.netIn, elapsedSeconds)
			sample.NetOutBytesPerSec = rateBytesPerSecond(p.previous.netOut, current.netOut, elapsedSeconds)
			sample.DiskReadBytesPerSec = rateBytesPerSecond(p.previous.diskRead, current.diskRead, elapsedSeconds)
			sample.DiskWriteBytesPerSec = rateBytesPerSecond(p.previous.diskWrite, current.diskWrite, elapsedSeconds)
			sample.DiskBusyPct = diskBusyPct(*p.previous, *current)
		}
	}

	p.previous = current
	return sample, nil
}

func defaultStatFS(path string) (FilesystemStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return FilesystemStats{}, err
	}
	return FilesystemStats{
		Blocks: stat.Blocks,
		Bfree:  stat.Bfree,
		Files:  stat.Files,
		Ffree:  stat.Ffree,
	}, nil
}

func parseLoadAvg(raw []byte) (float64, float64, float64, error) {
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("parse /proc/loadavg: insufficient fields")
	}
	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse load1: %w", err)
	}
	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse load5: %w", err)
	}
	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse load15: %w", err)
	}
	return load1, load5, load15, nil
}

func parseMemInfo(raw []byte) (float64, int64, float64, error) {
	values := map[string]uint64{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse %s: %w", key, err)
		}
		values[key] = value
	}
	memTotal := values["MemTotal"]
	memAvailable := values["MemAvailable"]
	if memTotal == 0 {
		return 0, 0, 0, fmt.Errorf("parse /proc/meminfo: MemTotal missing")
	}
	memUsedPct := float64(memTotal-memAvailable) / float64(memTotal) * 100
	memAvailableBytes := int64(memAvailable * 1024)

	swapTotal := values["SwapTotal"]
	swapFree := values["SwapFree"]
	swapUsedPct := 0.0
	if swapTotal > 0 {
		swapUsedPct = float64(swapTotal-swapFree) / float64(swapTotal) * 100
	}
	return memUsedPct, memAvailableBytes, swapUsedPct, nil
}

func parseUptime(raw []byte) (int64, error) {
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("parse /proc/uptime: insufficient fields")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime: %w", err)
	}
	return int64(seconds), nil
}

func parseCPUStat(raw []byte) (total, idle, iowait, steal uint64, err error) {
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] != "cpu" {
			continue
		}
		for i := 1; i < len(fields); i++ {
			value, parseErr := strconv.ParseUint(fields[i], 10, 64)
			if parseErr != nil {
				return 0, 0, 0, 0, fmt.Errorf("parse cpu field %d: %w", i, parseErr)
			}
			total += value
			switch i {
			case 4:
				idle = value
			case 5:
				iowait = value
			case 8:
				steal = value
			}
		}
		return total, idle, iowait, steal, nil
	}
	return 0, 0, 0, 0, fmt.Errorf("parse /proc/stat: cpu line missing")
}

func parseNetDev(raw []byte) (uint64, uint64, error) {
	var totalIn, totalOut uint64
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		recv, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse net recv for %s: %w", iface, err)
		}
		transmit, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse net transmit for %s: %w", iface, err)
		}
		totalIn += recv
		totalOut += transmit
	}
	return totalIn, totalOut, nil
}

func parseDiskStats(raw []byte) (uint64, uint64, uint64, error) {
	var totalRead, totalWrite, totalBusy uint64
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		device := fields[2]
		if skipDiskDevice(device) {
			continue
		}
		readSectors, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse read sectors for %s: %w", device, err)
		}
		writeSectors, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse write sectors for %s: %w", device, err)
		}
		busyMS, err := strconv.ParseUint(fields[12], 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse io busy ms for %s: %w", device, err)
		}
		totalRead += readSectors * 512
		totalWrite += writeSectors * 512
		totalBusy += busyMS
	}
	return totalRead, totalWrite, totalBusy, nil
}

func skipDiskDevice(device string) bool {
	return strings.HasPrefix(device, "loop") || strings.HasPrefix(device, "ram") || strings.HasPrefix(device, "fd") || strings.HasPrefix(device, "dm-") || strings.HasPrefix(device, "md")
}

func deriveUsagePercents(stats FilesystemStats) (float64, float64) {
	diskUsedPct := 0.0
	if stats.Blocks > 0 {
		diskUsedPct = float64(stats.Blocks-stats.Bfree) / float64(stats.Blocks) * 100
	}
	inodeUsedPct := 0.0
	if stats.Files > 0 {
		inodeUsedPct = float64(stats.Files-stats.Ffree) / float64(stats.Files) * 100
	}
	return diskUsedPct, inodeUsedPct
}

func cpuUsagePct(previous, current snapshot) float64 {
	deltaTotal := current.cpuTotal - previous.cpuTotal
	if deltaTotal == 0 {
		return 0
	}
	deltaIdle := current.cpuIdle - previous.cpuIdle
	deltaIowait := current.cpuIowait - previous.cpuIowait
	busy := deltaTotal - deltaIdle - deltaIowait
	return float64(busy) / float64(deltaTotal) * 100
}

func cpuIowaitPct(previous, current snapshot) float64 {
	deltaTotal := current.cpuTotal - previous.cpuTotal
	if deltaTotal == 0 {
		return 0
	}
	return float64(current.cpuIowait-previous.cpuIowait) / float64(deltaTotal) * 100
}

func cpuStealPct(previous, current snapshot) float64 {
	deltaTotal := current.cpuTotal - previous.cpuTotal
	if deltaTotal == 0 {
		return 0
	}
	return float64(current.cpuSteal-previous.cpuSteal) / float64(deltaTotal) * 100
}

func rateBytesPerSecond(previous, current uint64, elapsedSeconds float64) int64 {
	if elapsedSeconds <= 0 || current < previous {
		return 0
	}
	return int64(float64(current-previous) / elapsedSeconds)
}

func diskBusyPct(previous, current snapshot) float64 {
	elapsedMillis := current.observedAt.Sub(previous.observedAt).Milliseconds()
	if elapsedMillis <= 0 || current.diskBusyMS < previous.diskBusyMS {
		return 0
	}
	pct := float64(current.diskBusyMS-previous.diskBusyMS) / float64(elapsedMillis) * 100
	if pct > 100 {
		return 100
	}
	return pct
}
