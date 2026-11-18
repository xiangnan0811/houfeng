package deletionledger

import (
	"context"
	"fmt"
	"reflect"

	"houfeng/internal/center/recorddeletion"
)

// SourceRetirementFence is a trusted maintenance dependency. Its phase scope
// keeps the original offline fence alive and quiescent across each effect.
type SourceRetirementFence interface {
	SourceRetirementPhase(context.Context) (uint64, error)
	WithSourceRetirementFence(context.Context, uint64, func(context.Context) error) error
	VerifySourceRetirementProjection(context.Context, SourceRetirementRequest) error
	Trust() (recorddeletion.LedgerVerificationTrust, error)
}

type SourceRetirementCommitVerifier struct{ fence SourceRetirementFence }

// SourceRetirementCommit can only be minted after the trusted dependency has
// read back the committed APP projection and its original provenance.
type SourceRetirementCommit struct {
	request  SourceRetirementRequest
	fence    SourceRetirementFence
	phase    uint64
	verified bool
}

func sourceNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func NewSourceRetirementCommitVerifier(fence SourceRetirementFence) (*SourceRetirementCommitVerifier, error) {
	if sourceNil(fence) {
		return nil, sourceInvalid("commit verifier fence")
	}
	return &SourceRetirementCommitVerifier{fence: fence}, nil
}

func (verifier *SourceRetirementCommitVerifier) Verify(ctx context.Context, request SourceRetirementRequest) (SourceRetirementCommit, error) {
	request = cloneSourceRequest(request)
	if ctx == nil || verifier == nil || sourceNil(verifier.fence) || request.Validate() != nil {
		return SourceRetirementCommit{}, sourceInvalid("commit verifier request")
	}
	phase, err := verifier.fence.SourceRetirementPhase(ctx)
	if err != nil {
		return SourceRetirementCommit{}, err
	}
	if phase == 0 {
		return SourceRetirementCommit{}, sourceInvalid("fence phase")
	}
	err = verifier.fence.WithSourceRetirementFence(ctx, phase, func(held context.Context) error {
		if held == nil {
			return sourceInvalid("fence context")
		}
		if err := held.Err(); err != nil {
			return err
		}
		return verifier.fence.VerifySourceRetirementProjection(held, cloneSourceRequest(request))
	})
	if err != nil {
		return SourceRetirementCommit{}, err
	}
	return SourceRetirementCommit{request: request, fence: verifier.fence, phase: phase, verified: true}, nil
}

func (commit SourceRetirementCommit) Valid() bool {
	return commit.verified && commit.phase != 0 && !sourceNil(commit.fence) && commit.request.Validate() == nil
}

func (commit SourceRetirementCommit) Request() SourceRetirementRequest {
	if !commit.Valid() {
		return SourceRetirementRequest{}
	}
	return cloneSourceRequest(commit.request)
}

func (commit SourceRetirementCommit) withProjection(ctx context.Context, apply func(context.Context) error) error {
	if ctx == nil || !commit.Valid() || apply == nil {
		return sourceInvalid("commit capability")
	}
	return commit.fence.WithSourceRetirementFence(ctx, commit.phase, func(held context.Context) error {
		if held == nil {
			return sourceInvalid("fence context")
		}
		if err := held.Err(); err != nil {
			return err
		}
		if err := commit.fence.VerifySourceRetirementProjection(held, cloneSourceRequest(commit.request)); err != nil {
			return fmt.Errorf("source committed projection: %w", err)
		}
		if err := held.Err(); err != nil {
			return err
		}
		return apply(held)
	})
}

// sameSourceFence requires the continuity guard in trust to be the exact held
// concrete fence, not another object claiming a matching generation.
func sameSourceFence(fence SourceRetirementFence, guard recorddeletion.LedgerContinuityGuard) bool {
	if sourceNil(fence) || sourceNil(guard) {
		return false
	}
	a, b := reflect.ValueOf(fence), reflect.ValueOf(guard)
	return a.Kind() == reflect.Pointer && b.Kind() == reflect.Pointer && a.Type() == b.Type() && a.Pointer() == b.Pointer()
}
