//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	runToken        = "ddac5da9673f4c9ba7d2ad68570fd82d"
	maxFrame        = 16 << 10
	maxGenesis      = 512 << 10
	serviceUID      = 10002
	serviceGID      = 10002
	launcherPath    = "/opt/g2/c80-launcher"
	predecessorPath = "/opt/g2/c80-predecessor"
	adminPath       = "/opt/g2/houfeng-record-platform-admin"
	guardPath       = "/var/lib/g2/control/maintenance/guard/maintenance.lock"
	ledgerPath      = "/var/lib/g2/control/maintenance/fences/record-ledger"
	realmPath       = "/var/lib/g2/realm-control"
	childCgroupPath = "/sys/fs/cgroup/g2-c80-predecessor"
)

var denied = errors.New("launch denied")

type application struct {
	ID               string `json:"ID"`
	SystemIdentifier string `json:"SystemIdentifier"`
	DatabaseName     string `json:"DatabaseName"`
	DatabaseOID      uint32 `json:"DatabaseOID"`
	Epoch            uint64 `json:"Epoch"`
}

type coordinator struct {
	PID              int    `json:"pid"`
	Starttime        uint64 `json:"starttime"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

type supervisor struct {
	PID           int    `json:"pid"`
	Starttime     uint64 `json:"starttime"`
	ProgramSHA256 string `json:"program_sha256"`
}

type fileIdentity struct {
	Dev uint64 `json:"dev"`
	Ino uint64 `json:"ino"`
}

type cgroupIdentity struct {
	Path string `json:"path"`
	Dev  uint64 `json:"dev"`
	Ino  uint64 `json:"ino"`
}

type launchIntent struct {
	Version           uint64         `json:"version"`
	PhaseGeneration   uint64         `json:"phase_generation"`
	RecoveryID        string         `json:"recovery_id"`
	Application       application    `json:"application"`
	RealmDigest       string         `json:"realm_digest"`
	Coordinator       coordinator    `json:"coordinator"`
	Guard             fileIdentity   `json:"guard"`
	ConfigFD          int            `json:"config_fd"`
	ConfigDigest      string         `json:"config_digest"`
	ActorUserID       string         `json:"actor_user_id"`
	RecordID          string         `json:"record_id"`
	ChildCgroup       cgroupIdentity `json:"child_cgroup"`
	LauncherSHA256    string         `json:"launcher_sha256"`
	PredecessorSHA256 string         `json:"predecessor_sha256"`
}

type LaunchIntent = launchIntent

type launchPermit struct {
	LaunchIntent
	RunToken   string     `json:"run_token"`
	BootID     string     `json:"boot_id"`
	Supervisor supervisor `json:"supervisor"`
}

type serviceConfig struct {
	Version         uint64 `json:"version"`
	PhaseGeneration uint64 `json:"phase_generation"`
	RuntimeURL      string `json:"runtime_url"`
	DeploymentID    string `json:"deployment_id"`
	ProjectID       string `json:"project_id"`
	ActorUserID     string `json:"actor_user_id"`
	RecordID        string `json:"record_id"`
}

type maintenancePhase struct {
	Version     uint64      `json:"version"`
	Generation  uint64      `json:"generation"`
	Previous    [32]uint8   `json:"previous"`
	RecoveryID  string      `json:"recovery_id"`
	Application application `json:"application"`
	Phase       string      `json:"phase"`
}

var intentKeys = []string{"version", "phase_generation", "recovery_id", "application", "realm_digest", "coordinator", "guard", "config_fd", "config_digest", "actor_user_id", "record_id", "child_cgroup", "launcher_sha256", "predecessor_sha256"}
var applicationKeys = []string{"ID", "SystemIdentifier", "DatabaseName", "DatabaseOID", "Epoch"}

// checkJSON rejects duplicate keys at every depth and does not accept a second
// value or invalid UTF-8. json.Unmarshal alone silently accepts both duplicate
// keys and invalid UTF-8, neither of which is valid for this protocol.
func checkJSON(raw []byte, allowNull bool) error {
	return checkJSONBounded(raw, allowNull, maxFrame)
}

func checkJSONBounded(raw []byte, allowNull bool, limit int) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return denied
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := checkValue(decoder, 0, allowNull); err != nil {
		return denied
	}
	if _, err := decoder.Token(); err != io.EOF {
		return denied
	}
	return nil
}

func checkValue(decoder *json.Decoder, depth int, allowNull bool) error {
	if depth > 64 {
		return denied
	}
	token, err := decoder.Token()
	if err != nil || token == nil && !allowNull {
		return denied
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return denied
			}
			if _, exists := seen[key]; exists {
				return denied
			}
			seen[key] = struct{}{}
			if err := checkValue(decoder, depth+1, allowNull); err != nil {
				return err
			}
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return denied
		}
	case '[':
		for decoder.More() {
			if err := checkValue(decoder, depth+1, allowNull); err != nil {
				return err
			}
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return denied
		}
	default:
		return denied
	}
	return nil
}

func exactObject(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	if err := checkJSON(raw, false); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		return nil, denied
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return nil, denied
		}
	}
	return fields, nil
}

func validateApplication(raw []byte, value application) error {
	if _, err := exactObject(raw, applicationKeys...); err != nil {
		return err
	}
	if !shortText(value.ID) || !shortText(value.SystemIdentifier) || !shortText(value.DatabaseName) || value.DatabaseOID == 0 || value.Epoch == 0 || value.Epoch > math.MaxInt64 {
		return denied
	}
	return nil
}

func shortText(value string) bool {
	return len(value) > 0 && len(value) <= 256 && !strings.ContainsRune(value, 0)
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return value != strings.Repeat("0", 64)
}

func parseConfig(raw []byte) (serviceConfig, error) {
	var config serviceConfig
	_, err := exactObject(raw, "version", "phase_generation", "runtime_url", "deployment_id", "project_id", "actor_user_id", "record_id")
	if err != nil || json.Unmarshal(raw, &config) != nil || config.Version != 1 || config.PhaseGeneration == 0 || config.PhaseGeneration > math.MaxInt64 || len(config.RuntimeURL) == 0 || len(config.RuntimeURL) > 8192 || !shortText(config.DeploymentID) || !shortText(config.ProjectID) || !shortText(config.ActorUserID) || !shortText(config.RecordID) {
		return serviceConfig{}, denied
	}
	return config, nil
}

func parseIntent(raw []byte, permit bool) (launchIntent, launchPermit, error) {
	keys := append([]string(nil), intentKeys...)
	if permit {
		keys = append(keys, "run_token", "boot_id", "supervisor")
	}
	fields, err := exactObject(raw, keys...)
	if err != nil {
		return launchIntent{}, launchPermit{}, denied
	}
	for key, names := range map[string][]string{
		"coordinator":  {"pid", "starttime", "executable_sha256"},
		"guard":        {"dev", "ino"},
		"child_cgroup": {"path", "dev", "ino"},
	} {
		if _, err := exactObject(fields[key], names...); err != nil {
			return launchIntent{}, launchPermit{}, denied
		}
	}
	var intent launchIntent
	if json.Unmarshal(raw, &intent) != nil || validateApplication(fields["application"], intent.Application) != nil {
		return launchIntent{}, launchPermit{}, denied
	}
	if intent.Version != 1 || intent.PhaseGeneration == 0 || intent.PhaseGeneration > math.MaxInt64 || !shortText(intent.RecoveryID) || !shortText(intent.ActorUserID) || !shortText(intent.RecordID) || intent.ConfigFD < 0 || intent.ConfigFD > math.MaxInt32 || intent.Coordinator.PID <= 1 || intent.Coordinator.Starttime == 0 || intent.Guard.Ino == 0 || intent.ChildCgroup.Path != childCgroupPath || intent.ChildCgroup.Ino == 0 || !validDigest(intent.RealmDigest) || !validDigest(intent.ConfigDigest) || !validDigest(intent.LauncherSHA256) || !validDigest(intent.PredecessorSHA256) || !validDigest(intent.Coordinator.ExecutableSHA256) {
		return launchIntent{}, launchPermit{}, denied
	}
	var result launchPermit
	if permit {
		if _, err := exactObject(fields["supervisor"], "pid", "starttime", "program_sha256"); err != nil || json.Unmarshal(raw, &result) != nil || result.RunToken != runToken || !validBootID(result.BootID) || result.Supervisor.PID <= 1 || result.Supervisor.Starttime == 0 || !validDigest(result.Supervisor.ProgramSHA256) {
			return launchIntent{}, launchPermit{}, denied
		}
		if result.LaunchIntent != intent {
			return launchIntent{}, launchPermit{}, denied
		}
	}
	return intent, result, nil
}

func parsePhase(raw []byte) (maintenancePhase, error) {
	var phase maintenancePhase
	fields, err := exactObject(raw, "version", "generation", "previous", "recovery_id", "application", "phase")
	if err != nil || json.Unmarshal(raw, &phase) != nil || validateApplication(fields["application"], phase.Application) != nil || phase.Version != 1 || phase.Generation == 0 || phase.Generation > math.MaxInt64 || !shortText(phase.RecoveryID) {
		return maintenancePhase{}, denied
	}
	// Arrays decoded into fixed arrays otherwise accept a short JSON list.
	var previous []json.RawMessage
	if json.Unmarshal(fields["previous"], &previous) != nil || len(previous) != 32 {
		return maintenancePhase{}, denied
	}
	return phase, nil
}
