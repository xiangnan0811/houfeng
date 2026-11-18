//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
)

var ordinaryLongRunningServices = [...]string{"clamav", "houfeng-record-authority", "houfeng", "houfeng-content-processor"}
var ordinaryOneShotServices = [...]string{"houfeng-storage-init", "houfeng-secrets-init", "houfeng-db-init", "houfeng-local-namespace-init"}

type ordinaryRuntimeRecipeService struct {
	Image    string `json:"image"`
	Restart  string `json:"restart"`
	Networks map[string]struct {
		Aliases []string `json:"aliases"`
	} `json:"networks"`
	Ports []struct {
		Target    int    `json:"target"`
		Published string `json:"published"`
		HostIP    string `json:"host_ip"`
		Protocol  string `json:"protocol"`
	} `json:"ports"`
}

func ordinaryRecipeRestart(value string) (fenceRestart, bool) {
	if value == "" {
		value = "no"
	}
	name, count, hasCount := strings.Cut(value, ":")
	if name != "no" && name != "always" && name != "unless-stopped" && name != "on-failure" {
		return fenceRestart{}, false
	}
	result := fenceRestart{Name: name}
	if hasCount {
		if name != "on-failure" {
			return fenceRestart{}, false
		}
		n, err := strconv.Atoi(count)
		if err != nil || n < 0 || strconv.Itoa(n) != count {
			return fenceRestart{}, false
		}
		result.MaximumRetryCount = n
	}
	return result, true
}

// Proof covers recipe-derived restart policies and image/config identities as
// well as the absence of any active pre-fence container. Compose config output
// can include environment values and must never be logged or persisted here.
func ordinaryVerifyComposeRuntime(ctx context.Context, intent ordinaryUpgradeIntent) error {
	override := filepath.Join(intent.Request.WorkingDirectory, "maintenance", "ordinary-operations", intent.Request.OperationID, "compose-override.yaml")
	body, err := ordinaryComposeCommand(ctx, intent, override, "config", "--format", "json")
	if err != nil {
		return errOrdinaryUpgrade
	}
	var config struct {
		Services map[string]ordinaryRuntimeRecipeService `json:"services"`
		Networks map[string]struct {
			Name string `json:"name"`
		} `json:"networks"`
	}
	err = json.Unmarshal(body, &config)
	clear(body)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !ordinaryDatabaseRecipeMatches(intent, config.Services) {
		return errOrdinaryUpgrade
	}
	originalDB, foundDB := ordinaryIntentDatabase(intent)
	database, err := fenceInspect(ctx, intent.Request.DatabaseContainerID)
	if !foundDB || err != nil || database.state != "running" || !fenceSameAnchors(originalDB, database.anchor) || !ordinaryExactNetworks(originalDB.Networks, database.anchor.Networks) || database.anchor.Restart != originalDB.Restart {
		return errOrdinaryUpgrade
	}
	_, databaseHealth, err := ordinaryRuntimePIDAndHealth(ctx, intent.Request.DatabaseContainerID)
	if err != nil || databaseHealth != "healthy" {
		return errOrdinaryUpgrade
	}
	services := append(append([]string(nil), ordinaryLongRunningServices[:]...), ordinaryOneShotServices[:]...)
	hashBody, err := ordinaryComposeCommand(ctx, intent, override, "config", "--hash", strings.Join(services, ","))
	if err != nil {
		return errOrdinaryUpgrade
	}
	hashes := make(map[string]string, len(services))
	for _, line := range strings.Split(strings.TrimSpace(string(hashBody)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !fenceID.MatchString(fields[1]) || hashes[fields[0]] != "" {
			return errOrdinaryUpgrade
		}
		hashes[fields[0]] = fields[1]
	}
	expectedImage := make(map[string]string, len(services))
	expectedRestart := make(map[string]fenceRestart, len(services))
	isOneShot := make(map[string]bool, len(ordinaryOneShotServices))
	for _, service := range ordinaryOneShotServices {
		isOneShot[service] = true
	}
	for _, service := range services {
		item, ok := config.Services[service]
		policy, valid := ordinaryRecipeRestart(item.Restart)
		if !ok || !valid || item.Image == "" || hashes[service] == "" || (isOneShot[service] && policy != (fenceRestart{Name: "no"})) || (!isOneShot[service] && policy.Name == "no") {
			return errOrdinaryUpgrade
		}
		image, err := fenceDocker(ctx, "image", "inspect", "--format", "{{.Id}}", item.Image)
		id := strings.TrimSpace(string(image))
		if err != nil || !strings.HasPrefix(id, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(id, "sha256:")) {
			return errOrdinaryUpgrade
		}
		expectedImage[service], expectedRestart[service] = id, policy
	}
	old := make(map[string]bool, len(intent.Fence.Containers))
	for _, container := range intent.Fence.Containers {
		if container.ID != intent.Request.DatabaseContainerID {
			old[container.ID] = true
		}
	}
	ids, err := fenceListContainers(ctx)
	if err != nil {
		return errOrdinaryUpgrade
	}
	found := make(map[string]bool, len(services))
	for _, id := range ids {
		item, err := fenceInspect(ctx, id)
		if err != nil {
			return errOrdinaryUpgrade
		}
		if old[id] && (item.state != "exited" && item.state != "created" || item.anchor.Restart != (fenceRestart{Name: "no"})) {
			return errOrdinaryUpgrade
		}
		if item.anchor.Project != intent.Request.Project {
			continue
		}
		image, selected := expectedImage[item.anchor.Service]
		if !selected || old[id] {
			continue
		}
		if found[item.anchor.Service] || item.anchor.WorkingDirectory != intent.Request.WorkingDirectory || item.anchor.Image != image || item.anchor.Restart != expectedRestart[item.anchor.Service] || item.scheduler {
			return errOrdinaryUpgrade
		}
		if item.anchor.Service == "houfeng" && ordinaryVerifyCenterIngress(ctx, id, item.networks, config.Services["houfeng"], config.Networks) != nil {
			return errOrdinaryUpgrade
		}
		if isOneShot[item.anchor.Service] {
			started, restarts, code, err := ordinaryOneShotState(ctx, id)
			if err != nil || item.state != "exited" || started.IsZero() || restarts != 0 || code != 0 {
				return errOrdinaryUpgrade
			}
		} else {
			pid, health, err := ordinaryRuntimePIDAndHealth(ctx, id)
			if err != nil || item.state != "running" || pid <= 0 || (health != "" && health != "healthy") {
				return errOrdinaryUpgrade
			}
		}
		observedHash, err := fenceDocker(ctx, "inspect", "--format", `{{index .Config.Labels "com.docker.compose.config-hash"}}`, id)
		if err != nil || strings.TrimSpace(string(observedHash)) != hashes[item.anchor.Service] {
			return errOrdinaryUpgrade
		}
		found[item.anchor.Service] = true
	}
	if len(found) != len(services) {
		return errOrdinaryUpgrade
	}
	return nil
}

// Compare actual Center attachments and published bindings to the merged recipe.
func ordinaryCenterNetworksMatch(actual map[string]fenceNetwork, center ordinaryRuntimeRecipeService, networks map[string]struct {
	Name string `json:"name"`
}) bool {
	if len(actual) != len(center.Networks) {
		return false
	}
	for key, expected := range center.Networks {
		name := networks[key].Name
		observed, ok := actual[name]
		if name == "" || !ok {
			return false
		}
		for _, alias := range expected.Aliases {
			found := false
			for _, got := range observed.Aliases {
				if got == alias {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func ordinaryVerifyCenterIngress(ctx context.Context, id string, actual map[string]fenceNetwork, center ordinaryRuntimeRecipeService, networks map[string]struct {
	Name string `json:"name"`
}) error {
	if !ordinaryCenterNetworksMatch(actual, center, networks) {
		return errOrdinaryUpgrade
	}
	type binding struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	expected := make(map[string][]binding)
	for _, port := range center.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		key := strconv.Itoa(port.Target) + "/" + protocol
		if port.Target <= 0 || port.Published == "" || port.HostIP == "" {
			return errOrdinaryUpgrade
		}
		expected[key] = append(expected[key], binding{HostIP: port.HostIP, HostPort: port.Published})
	}
	body, err := fenceDocker(ctx, "inspect", "--format", "{{json .HostConfig.PortBindings}}", id)
	var got map[string][]binding
	if err != nil || json.Unmarshal(body, &got) != nil || len(got) != len(expected) {
		return errOrdinaryUpgrade
	}
	for key, want := range expected {
		entries := got[key]
		if len(entries) != len(want) {
			return errOrdinaryUpgrade
		}
		for _, binding := range want {
			count := 0
			for _, entry := range entries {
				if entry == binding {
					count++
				}
			}
			if count != 1 {
				return errOrdinaryUpgrade
			}
		}
	}
	return nil
}
