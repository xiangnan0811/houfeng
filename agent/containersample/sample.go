// Package containersample collects Docker container metadata from the local
// Docker daemon via the docker CLI. It is intentionally thin — no Docker SDK
// dependency — and silently returns nil when Docker is unavailable.
package containersample

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
)

// Collect enumerates containers via "docker ps" and attaches per-container
// CPU/memory percentages via "docker stats --no-stream". If the docker CLI
// is not found or the daemon is unreachable, Collect returns (nil, nil) so
// callers can silently skip container data.
func Collect(ctx context.Context) ([]agentapi.ContainerInfo, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	psOut, err := runDockerPS(ctx)
	if err != nil {
		return nil, nil
	}

	containers := parseDockerPS(psOut)
	if len(containers) == 0 {
		return nil, nil
	}

	statsOut, err := runDockerStats(ctx)
	if err != nil {
		// Stats are optional — if stats fail we still return ps info.
		return containers, nil
	}

	statsMap := parseDockerStats(statsOut)
	for i := range containers {
		if s, ok := statsMap[containers[i].Name]; ok {
			if s.cpuPct != nil {
				v := *s.cpuPct
				containers[i].CPUPct = &v
			}
			if s.memPct != nil {
				v := *s.memPct
				containers[i].MemPct = &v
			}
		}
	}

	return containers, nil
}

func runDockerPS(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx,
		"docker", "ps",
		"--all", "--no-trunc",
		"--format", `{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}`,
	).Output()
}

func runDockerStats(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx,
		"docker", "stats",
		"--no-stream",
		"--format", `{{.CPUPerc}}\t{{.MemPerc}}\t{{.Name}}`,
	).Output()
}

func parseDockerPS(data []byte) []agentapi.ContainerInfo {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	var containers []agentapi.ContainerInfo
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			continue
		}
		id := fields[0]
		name := fields[1]
		image := fields[2]
		status := normalizeStatus(fields[3])

		containers = append(containers, agentapi.ContainerInfo{
			ID:     id,
			Name:   name,
			Image:  image,
			Status: status,
		})
	}
	return containers
}

// normalizeStatus maps the full "Up 2 hours" / "Exited (0) 3 days ago"
// docker ps status string to a short canonical value.
func normalizeStatus(status string) string {
	lower := strings.ToLower(status)
	// "Up ... (Paused)" must be checked before the generic "up " prefix.
	if strings.HasPrefix(lower, "up ") && strings.Contains(lower, "(paused)") {
		return "paused"
	}
	if strings.HasPrefix(lower, "up ") {
		return "running"
	}
	if strings.HasPrefix(lower, "exited") {
		return "exited"
	}
	if strings.HasPrefix(lower, "created") {
		return "created"
	}
	if strings.HasPrefix(lower, "restarting") {
		return "restarting"
	}
	if strings.HasPrefix(lower, "removing") {
		return "removing"
	}
	if strings.HasPrefix(lower, "dead") {
		return "dead"
	}
	return status
}

type statsEntry struct {
	cpuPct *float64
	memPct *float64
}

func parseDockerStats(data []byte) map[string]statsEntry {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	result := make(map[string]statsEntry)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		rawCPU := strings.TrimSuffix(strings.TrimSpace(fields[0]), "%")
		rawMem := strings.TrimSuffix(strings.TrimSpace(fields[1]), "%")
		name := strings.TrimSpace(fields[2])

		entry := statsEntry{}
		if v, err := strconv.ParseFloat(rawCPU, 64); err == nil {
			entry.cpuPct = &v
		}
		if v, err := strconv.ParseFloat(rawMem, 64); err == nil {
			entry.memPct = &v
		}
		result[name] = entry
	}
	return result
}
