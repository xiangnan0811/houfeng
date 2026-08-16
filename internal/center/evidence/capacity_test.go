package evidence

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestCapacityEnforcerReadsOnlyEvidenceUsageAndFailsClosed(t *testing.T) {
	t.Parallel()

	source := &projectCapacitySourceStub{usage: ProjectCapacityUsage{
		ProjectID: "default", LogicalSnapshotCount: 1, LogicalSnapshotBytes: 700,
		PhysicalPayloadCount: 1, PhysicalCanonicalBytes: 500, PhysicalCompressedBytes: 300,
	}}
	enforcer, err := NewCapacityEnforcer(CapacityPolicy{ProjectLimitBytes: 1_000, WarningPercent: 80}, source)
	if err != nil {
		t.Fatalf("NewCapacityEnforcer() error = %v", err)
	}
	evaluation, err := enforcer.Evaluate(context.Background(), "default", 100)
	if err != nil || evaluation.Outcome.Status != QuotaWarning || source.calls != 1 {
		t.Fatalf("Evaluate() = %#v, %v; source calls=%d", evaluation, err, source.calls)
	}

	source.err = errors.New("database unavailable digest=secret")
	evaluation, err = enforcer.Evaluate(context.Background(), "default", 1)
	if err != nil || evaluation.Outcome.Status != QuotaUnavailable ||
		evaluation.Outcome.Reason != "project evidence capacity unavailable" {
		t.Fatalf("Evaluate(unavailable) = %#v, %v", evaluation, err)
	}
}

func TestCapacityEnforcerRejectsTypedNilAndInconsistentUsage(t *testing.T) {
	t.Parallel()

	var typedNil *projectCapacitySourceStub
	if _, err := NewCapacityEnforcer(DefaultCapacityPolicy(), typedNil); !errors.Is(err, ErrCapacityUnavailable) {
		t.Fatalf("NewCapacityEnforcer(typed nil) error = %v", err)
	}
	source := &projectCapacitySourceStub{usage: ProjectCapacityUsage{ProjectID: "other"}}
	enforcer, err := NewCapacityEnforcer(DefaultCapacityPolicy(), source)
	if err != nil {
		t.Fatalf("NewCapacityEnforcer() error = %v", err)
	}
	evaluation, err := enforcer.Evaluate(context.Background(), "default", 1)
	if err != nil || evaluation.Outcome.Status != QuotaUnavailable {
		t.Fatalf("Evaluate(inconsistent) = %#v, %v", evaluation, err)
	}
}

func TestProjectCapacityUsageRejectsImpossibleLogicalPhysicalAccounting(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		usage ProjectCapacityUsage
	}{
		{
			name: "logical snapshot without payload",
			usage: ProjectCapacityUsage{
				ProjectID: "default", LogicalSnapshotCount: 1, LogicalSnapshotBytes: 100,
			},
		},
		{
			name: "more distinct payloads than snapshots",
			usage: ProjectCapacityUsage{
				ProjectID: "default", LogicalSnapshotCount: 1, LogicalSnapshotBytes: 100,
				PhysicalPayloadCount: 2, PhysicalCanonicalBytes: 100, PhysicalCompressedBytes: 50,
			},
		},
		{
			name: "distinct canonical bytes exceed logical bytes",
			usage: ProjectCapacityUsage{
				ProjectID: "default", LogicalSnapshotCount: 1, LogicalSnapshotBytes: 100,
				PhysicalPayloadCount: 1, PhysicalCanonicalBytes: 101, PhysicalCompressedBytes: 50,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.usage.Validate("default"); !errors.Is(err, ErrCapacityUnavailable) {
				t.Fatalf("Validate() error = %v, want ErrCapacityUnavailable", err)
			}
		})
	}
}

func TestEvidenceCapacityPolicyDefaultAndAdjustedValidation(t *testing.T) {
	t.Parallel()

	policy := DefaultCapacityPolicy()
	if policy.ProjectLimitBytes != 10*1024*1024*1024 || policy.WarningPercent != 80 {
		t.Fatalf("DefaultCapacityPolicy() = %#v", policy)
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("default policy validation error = %v", err)
	}

	adjusted := CapacityPolicy{ProjectLimitBytes: 64 * 1024, WarningPercent: 75}
	if err := adjusted.Validate(); err != nil {
		t.Fatalf("adjusted policy validation error = %v", err)
	}
	for _, invalid := range []CapacityPolicy{
		{},
		{ProjectLimitBytes: 1, WarningPercent: 0},
		{ProjectLimitBytes: 1, WarningPercent: 101},
		{ProjectLimitBytes: math.MaxUint64, WarningPercent: 80},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidCapacityPolicy) {
			t.Fatalf("CapacityPolicy(%#v).Validate() error = %v, want ErrInvalidCapacityPolicy", invalid, err)
		}
	}
}

func TestEvidenceCapacityPolicyRoundsWarningThresholdUp(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		policy    CapacityPolicy
		threshold uint64
	}{
		{name: "single byte", policy: CapacityPolicy{ProjectLimitBytes: 1, WarningPercent: 80}, threshold: 1},
		{name: "non integral percentage", policy: CapacityPolicy{ProjectLimitBytes: 101, WarningPercent: 80}, threshold: 81},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.policy.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			threshold, err := test.policy.WarningThresholdBytes()
			if err != nil || threshold != test.threshold {
				t.Fatalf("WarningThresholdBytes() = %d, %v; want %d", threshold, err, test.threshold)
			}
		})
	}
}

func TestCapacityEnforcerRejectsUnknownProjectBeforeSourceRead(t *testing.T) {
	t.Parallel()

	source := &projectCapacitySourceStub{}
	enforcer, err := NewCapacityEnforcer(DefaultCapacityPolicy(), source)
	if err != nil {
		t.Fatalf("NewCapacityEnforcer() error = %v", err)
	}
	if _, err := enforcer.Evaluate(context.Background(), "future-project", 1); !errors.Is(err, ErrCapacityUnavailable) {
		t.Fatalf("Evaluate(unknown project) error = %v, want ErrCapacityUnavailable", err)
	}
	if source.calls != 0 {
		t.Fatalf("unknown project source calls = %d, want zero", source.calls)
	}
}

func TestEvidenceCapacityPolicyEvaluationMatrix(t *testing.T) {
	t.Parallel()

	policy := CapacityPolicy{ProjectLimitBytes: 1_000, WarningPercent: 80}
	tests := []struct {
		name       string
		used       uint64
		additional uint64
		wantStatus QuotaStatus
		wantTotal  uint64
	}{
		{name: "below warning", used: 700, additional: 99, wantStatus: QuotaAllowed, wantTotal: 799},
		{name: "warning threshold exact", used: 700, additional: 100, wantStatus: QuotaWarning, wantTotal: 800},
		{name: "warning", used: 850, additional: 50, wantStatus: QuotaWarning, wantTotal: 900},
		{name: "quota boundary exact", used: 999, additional: 1, wantStatus: QuotaWarning, wantTotal: 1_000},
		{name: "quota exceeded", used: 1_000, additional: 1, wantStatus: QuotaExceeded, wantTotal: 1_001},
		{name: "policy reduced below existing usage", used: 1_001, additional: 0, wantStatus: QuotaExceeded, wantTotal: 1_001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := policy.Evaluate(tt.used, tt.additional)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got.Outcome.Status != tt.wantStatus || got.ProjectedBytes != tt.wantTotal ||
				got.WarningThresholdBytes != 800 || got.LimitBytes != 1_000 {
				t.Fatalf("Evaluate() = %#v", got)
			}
			if got.Outcome.Status == QuotaAllowed && got.Outcome.Reason != "" {
				t.Fatalf("allowed reason = %q, want empty", got.Outcome.Reason)
			}
			if got.Outcome.Status != QuotaAllowed && got.Outcome.Reason == "" {
				t.Fatal("non-allowed outcome reason is empty")
			}
		})
	}
}

func TestEvidenceCapacityPolicyRejectsOverflow(t *testing.T) {
	t.Parallel()

	policy := CapacityPolicy{ProjectLimitBytes: math.MaxInt64, WarningPercent: 80}
	if _, err := policy.Evaluate(math.MaxUint64-4, 5); !errors.Is(err, ErrCapacityArithmetic) {
		t.Fatalf("Evaluate(overflow) error = %v, want ErrCapacityArithmetic", err)
	}
}

func FuzzEvidenceCapacityEvaluationDeterministic(f *testing.F) {
	f.Add(uint64(10_000), uint8(80), uint64(7_999), uint64(1))
	f.Add(uint64(10_000), uint8(80), uint64(10_000), uint64(1))
	f.Fuzz(func(t *testing.T, limit uint64, warning uint8, used uint64, additional uint64) {
		policy := CapacityPolicy{ProjectLimitBytes: limit, WarningPercent: warning}
		if policy.Validate() != nil {
			return
		}
		first, firstErr := policy.Evaluate(used, additional)
		second, secondErr := policy.Evaluate(used, additional)
		if !errors.Is(firstErr, secondErr) || first != second {
			t.Fatalf("nondeterministic evaluation: first=%#v/%v second=%#v/%v", first, firstErr, second, secondErr)
		}
		if firstErr == nil && first.ProjectedBytes < used {
			t.Fatalf("projected bytes wrapped: %#v", first)
		}
	})
}

type projectCapacitySourceStub struct {
	usage ProjectCapacityUsage
	err   error
	calls int
}

func (source *projectCapacitySourceStub) ReadProjectEvidenceCapacity(context.Context, string) (ProjectCapacityUsage, error) {
	source.calls++
	return source.usage, source.err
}
