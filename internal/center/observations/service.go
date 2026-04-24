package observations

import "context"

type Repository interface {
	RecordBatch(context.Context, BatchWrite) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Ingest(ctx context.Context, batch BatchWrite) error {
	return s.repo.RecordBatch(ctx, batch)
}
