package ipquality_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentipquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

func TestFileStateStoreRoundTripsCollectionState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ip-quality-state.json")
	store := agentipquality.NewFileStateStore(path)
	state := agentipquality.State{
		LastAttemptedAt: time.Date(2026, time.June, 8, 1, 2, 3, 0, time.UTC),
		LastSucceededAt: time.Date(2026, time.June, 8, 1, 3, 4, 0, time.UTC),
		LastStatus:      agentapi.IPQualityStatusSuccess,
	}

	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.LastAttemptedAt.Equal(state.LastAttemptedAt) ||
		!got.LastSucceededAt.Equal(state.LastSucceededAt) ||
		got.LastStatus != state.LastStatus {
		t.Fatalf("Load() = %#v, want %#v", got, state)
	}
}

func TestFileStateStoreMissingFileReturnsZeroState(t *testing.T) {
	store := agentipquality.NewFileStateStore(filepath.Join(t.TempDir(), "missing.json"))

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.LastAttemptedAt.IsZero() || !got.LastSucceededAt.IsZero() || got.LastStatus != "" {
		t.Fatalf("Load() = %#v, want zero state", got)
	}
}

func TestDueUsesSuccessfulCollectionTimestamp(t *testing.T) {
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)
	plan := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}

	tests := []struct {
		name  string
		state agentipquality.State
		want  bool
	}{
		{name: "never collected", state: agentipquality.State{}, want: true},
		{name: "recent success", state: agentipquality.State{LastSucceededAt: now.Add(-23 * time.Hour)}, want: false},
		{name: "old success", state: agentipquality.State{LastSucceededAt: now.Add(-25 * time.Hour)}, want: true},
		{name: "failed attempt does not defer indefinitely", state: agentipquality.State{LastAttemptedAt: now.Add(-time.Hour), LastStatus: agentapi.IPQualityStatusFailure}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentipquality.Due(plan, tt.state, now); got != tt.want {
				t.Fatalf("Due() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDueReturnsFalseForDisabledOrInvalidPlan(t *testing.T) {
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)
	if agentipquality.Due(nil, agentipquality.State{}, now) {
		t.Fatal("Due(nil) = true, want false")
	}
	if agentipquality.Due(&agentapi.IPQualityPlan{Enabled: false, FrequencySeconds: 86400}, agentipquality.State{}, now) {
		t.Fatal("Due(disabled) = true, want false")
	}
	if agentipquality.Due(&agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 0}, agentipquality.State{}, now) {
		t.Fatal("Due(invalid frequency) = true, want false")
	}
}
