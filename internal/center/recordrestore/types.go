package recordrestore

import (
	"context"
	"errors"
	"io"

	"houfeng/internal/center/recordbackup"
	"houfeng/internal/center/recordreadiness"
)

var (
	ErrInvalidRestoreRequest  = errors.New("invalid record restore request")
	ErrTargetNotEmpty         = errors.New("record restore target is not empty")
	ErrIncompatibleRestore    = errors.New("incompatible record restore contract")
	ErrMissingArtifact        = errors.New("record restore artifact is missing")
	ErrRestoreIncomplete      = errors.New("record restore incomplete")
	ErrRestoreUnavailable     = errors.New("record restore unavailable")
	ErrRestoreCleanupRequired = errors.New("record restore cleanup required")
	ErrRestoreNotReady        = errors.New("record restore target is not ready")
	ErrResurrectionBlocked    = errors.New("record restore would resurrect purged content")
)

type Step string

const (
	StepValidateTarget   Step = "validate_target"
	StepVerifyManifest   Step = "verify_manifest"
	StepStageBytes       Step = "stage_bytes"
	StepRestoreDatabase  Step = "restore_database"
	StepRestoreObjects   Step = "restore_objects"
	StepReplayDeletions  Step = "replay_deletions"
	StepRebuildSearch    Step = "rebuild_search"
	StepRebuildActivity  Step = "rebuild_activity"
	StepConvergeACL      Step = "converge_acl"
	StepVerifyAdapters   Step = "verify_adapters"
	StepPublishReadiness Step = "publish_readiness"
)

type Target interface {
	Empty(context.Context) (bool, error)
	RestoreDatabase(context.Context, io.Reader) error
	RestoreObject(context.Context, recordbackup.ArtifactRef, io.Reader) error
	Serving(context.Context) bool
	Workers(context.Context) bool
}

type ArtifactPresence interface {
	HasArtifact(string) bool
}

type ArtifactSource interface {
	Open(context.Context, recordbackup.ArtifactRef) (io.ReadCloser, error)
}

type ReplayAdapter interface {
	Kind() recordreadiness.CapabilityKind
	ReplayDeletions(context.Context, recordbackup.DeletionWatermark) error
}

type ProjectionRebuilder interface {
	RebuildSearch(context.Context) error
	RebuildActivity(context.Context) error
}

type ACLConverger interface {
	Converge(context.Context) error
}

type AdapterVerifier interface {
	Verify(context.Context) error
}

type ReadinessPublisher interface {
	Publish(context.Context, recordreadiness.StatusMatrix) error
}

type Options struct {
	Target      Target
	Source      ArtifactSource
	Replay      []ReplayAdapter
	Projections ProjectionRebuilder
	ACL         ACLConverger
	Verifier    AdapterVerifier
	Readiness   ReadinessPublisher
	Current     recordbackup.BuildIdentity
	PurgedKinds []string
}

type Request struct {
	Manifest recordbackup.Manifest
}

type Plan struct {
	steps []Step
}

func (plan Plan) Steps() []Step {
	return append([]Step(nil), plan.steps...)
}

type Result struct {
	ready bool
	steps []Step
}

func (result Result) Ready() bool { return result.ready }

func (result Result) Steps() []Step {
	return append([]Step(nil), result.steps...)
}

type CleanupReceipt struct {
	abortedSteps []Step
	workspaces   int
}

func (receipt CleanupReceipt) AbortedSteps() []Step {
	return append([]Step(nil), receipt.abortedSteps...)
}

func (receipt CleanupReceipt) ReleasedWorkspaces() int { return receipt.workspaces }

func requiredSteps() []Step {
	return []Step{
		StepValidateTarget,
		StepVerifyManifest,
		StepStageBytes,
		StepRestoreDatabase,
		StepRestoreObjects,
		StepReplayDeletions,
		StepRebuildSearch,
		StepRebuildActivity,
		StepConvergeACL,
		StepVerifyAdapters,
		StepPublishReadiness,
	}
}
