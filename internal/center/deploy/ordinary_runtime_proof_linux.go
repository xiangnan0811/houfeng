//go:build linux

package deploy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"houfeng/internal/center/recordbirth"
)

type ordinaryRuntimeProcess struct {
	Service          string `json:"service"`
	ContainerID      string `json:"container_id"`
	HostPID          int    `json:"host_pid"`
	StartTime        uint64 `json:"start_time"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

// This observes selected running container PIDs and their actual executable
// bytes. It does not claim that an idle processor has completed a job.
func ordinaryVerifySelectedProcesses(ctx context.Context, intent ordinaryUpgradeIntent) ([]ordinaryRuntimeProcess, error) {
	if ctx == nil || ordinaryValidateIntent(intent) != nil {
		return nil, errOrdinaryUpgrade
	}
	ids, err := fenceListContainers(ctx)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	wanted := map[string]struct {
		digest   string
		healthy  bool
		uid, gid int
	}{
		"houfeng":                   {intent.Target.CenterSHA256, true, 10001, 10001},
		"houfeng-content-processor": {intent.Target.ProcessorSHA256, false, 10001, 10001},
		"houfeng-record-authority":  {intent.Target.AdminSHA256, true, 10002, 10002},
	}
	found := make(map[string]ordinaryRuntimeProcess, len(wanted))
	for _, id := range ids {
		observed, err := fenceInspect(ctx, id)
		if err != nil {
			return nil, errOrdinaryUpgrade
		}
		if observed.anchor.Project != intent.Request.Project {
			continue
		}
		selected, ok := wanted[observed.anchor.Service]
		if !ok {
			continue
		}
		if _, duplicate := found[observed.anchor.Service]; duplicate || observed.anchor.WorkingDirectory != intent.Request.WorkingDirectory || observed.anchor.Image != intent.Request.TargetImageID || observed.state != "running" || observed.scheduler {
			return nil, errOrdinaryUpgrade
		}
		pid, health, err := ordinaryRuntimePIDAndHealth(ctx, id)
		if err != nil || pid <= 0 || (selected.healthy && health != "healthy") {
			return nil, errOrdinaryUpgrade
		}
		start, err := ledgerPredecessorStart(pid)
		if err != nil || !ordinaryRuntimeCredentials(pid, selected.uid, selected.gid) {
			return nil, errOrdinaryUpgrade
		}
		digest, err := recordbirth.DigestExecutable(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil || hex.EncodeToString(digest[:]) != selected.digest {
			return nil, errOrdinaryUpgrade
		}
		afterStart, err := ledgerPredecessorStart(pid)
		after, inspectErr := fenceInspect(ctx, id)
		afterPID, afterHealth, stateErr := ordinaryRuntimePIDAndHealth(ctx, id)
		if err != nil || inspectErr != nil || stateErr != nil || afterStart != start || afterPID != pid || after.state != "running" || !ordinaryRuntimeCredentials(pid, selected.uid, selected.gid) || !fenceSameAnchors(observed.anchor, after.anchor) || (selected.healthy && afterHealth != "healthy") {
			return nil, errOrdinaryUpgrade
		}
		found[observed.anchor.Service] = ordinaryRuntimeProcess{Service: observed.anchor.Service, ContainerID: id, HostPID: pid, StartTime: start, ExecutableSHA256: selected.digest}
	}
	result := make([]ordinaryRuntimeProcess, 0, len(wanted))
	for _, service := range []string{"houfeng", "houfeng-content-processor", "houfeng-record-authority"} {
		process, ok := found[service]
		if !ok {
			return nil, errOrdinaryUpgrade
		}
		result = append(result, process)
	}
	return result, nil
}

func ordinaryRuntimePIDAndHealth(ctx context.Context, id string) (int, string, error) {
	output, err := fenceDocker(ctx, "inspect", "--format", "{{json .State.Pid}}\t{{json .State.Health}}", id)
	if err != nil {
		return 0, "", errOrdinaryUpgrade
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "\t")
	if len(fields) != 2 {
		return 0, "", errOrdinaryUpgrade
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", errOrdinaryUpgrade
	}
	var health *struct {
		Status string `json:"Status"`
	}
	if err := json.Unmarshal([]byte(fields[1]), &health); err != nil {
		return 0, "", errOrdinaryUpgrade
	}
	if health == nil {
		return pid, "", nil
	}
	return pid, health.Status, nil
}

func ordinaryRuntimeCredentials(pid, uid, gid int) bool {
	body, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil || len(body) > 64<<10 {
		return false
	}
	wantUID, wantGID := strconv.Itoa(uid), strconv.Itoa(gid)
	seenUID, seenGID := false, false
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 {
			continue
		}
		switch fields[0] {
		case "Uid:":
			seenUID = fields[1] == wantUID && fields[2] == wantUID && fields[3] == wantUID && fields[4] == wantUID
		case "Gid:":
			seenGID = fields[1] == wantGID && fields[2] == wantGID && fields[3] == wantGID && fields[4] == wantGID
		}
	}
	return seenUID && seenGID
}
