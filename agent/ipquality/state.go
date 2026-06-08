package ipquality

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type State struct {
	LastAttemptedAt time.Time `json:"last_attempted_at,omitempty"`
	LastSucceededAt time.Time `json:"last_succeeded_at,omitempty"`
	LastStatus      string    `json:"last_status,omitempty"`
}

type StateStore interface {
	Load(context.Context) (State, error)
	Save(context.Context, State) error
}

type FileStateStore struct {
	path string
}

func NewFileStateStore(path string) *FileStateStore {
	return &FileStateStore{path: path}
}

func (s *FileStateStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, err
	}
	if len(payload) == 0 {
		return State{}, nil
	}
	var state State
	if err := json.Unmarshal(payload, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *FileStateStore) Save(ctx context.Context, state State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".ip-quality-state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	cleanup = false
	return syncDir(dir)
}

func Due(plan *agentapi.IPQualityPlan, state State, now time.Time) bool {
	if plan == nil || !plan.Enabled || plan.FrequencySeconds <= 0 {
		return false
	}
	if state.LastSucceededAt.IsZero() {
		return true
	}
	return !now.UTC().Before(state.LastSucceededAt.Add(time.Duration(plan.FrequencySeconds) * time.Second))
}

func syncDir(path string) error {
	if path == "" || path == "." {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
