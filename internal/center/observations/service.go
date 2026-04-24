package observations

import (
	"context"
	"errors"
	"fmt"
)

type Repository interface {
	RecordBatch(context.Context, BatchWrite) error
}

type ProbeMetadataRepository interface {
	GetProbeMetadata(context.Context, string) (ProbeMetadata, error)
}

var (
	ErrInvalidProbeObservation = errors.New("invalid probe observation")
	ErrProbeMetadataNotFound   = errors.New("probe metadata not found")
)

type Service struct {
	repo          Repository
	probeMetadata ProbeMetadataRepository
}

func NewService(repo Repository, probeMetadata ProbeMetadataRepository) *Service {
	return &Service{repo: repo, probeMetadata: probeMetadata}
}

func (s *Service) Ingest(ctx context.Context, batch BatchWrite) error {
	for _, observation := range batch.ProbeObservations {
		if err := s.validateProbeObservation(ctx, observation); err != nil {
			return err
		}
	}

	return s.repo.RecordBatch(ctx, batch)
}

func (s *Service) validateProbeObservation(ctx context.Context, observation ProbeObservationWrite) error {
	if err := ValidateProbeObservation(observation); err != nil {
		return err
	}

	if s.probeMetadata == nil {
		return fmt.Errorf("%w: probe metadata lookup unavailable", ErrInvalidProbeObservation)
	}

	metadata, err := s.probeMetadata.GetProbeMetadata(ctx, observation.ProbeItemID)
	if err != nil {
		if errors.Is(err, ErrProbeMetadataNotFound) {
			return fmt.Errorf("%w: probe_item_id %q not found", ErrInvalidProbeObservation, observation.ProbeItemID)
		}
		return fmt.Errorf("lookup probe metadata for %q: %w", observation.ProbeItemID, err)
	}

	if metadata.TargetID != observation.TargetID {
		return fmt.Errorf(
			"%w: probe_item_id %q belongs to target_id %q, got %q",
			ErrInvalidProbeObservation,
			observation.ProbeItemID,
			metadata.TargetID,
			observation.TargetID,
		)
	}
	if metadata.ProbeKind != observation.ProbeKind {
		return fmt.Errorf(
			"%w: probe_item_id %q expects probe_kind %q, got %q",
			ErrInvalidProbeObservation,
			observation.ProbeItemID,
			metadata.ProbeKind,
			observation.ProbeKind,
		)
	}

	return nil
}
