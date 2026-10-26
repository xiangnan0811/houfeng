package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"houfeng/internal/center/attachments"
)

func TestImportBlobGCObserverRejectsAbsentSnapshot(t *testing.T) {
	t.Parallel()
	digests := [][sha256.Size]byte{sha256.Sum256([]byte("import"))}
	for name, test := range map[string]struct {
		repository *PostgresAttachmentRepository
		ctx        context.Context
		digests    [][sha256.Size]byte
	}{
		"nil_repository":      {repository: nil, ctx: context.Background(), digests: digests},
		"missing_transaction": {repository: &PostgresAttachmentRepository{}, ctx: context.Background(), digests: digests},
		"nil_context":         {repository: &PostgresAttachmentRepository{}, ctx: nil, digests: digests},
		"empty_input":         {repository: &PostgresAttachmentRepository{}, ctx: context.Background()},
	} {
		t.Run(name, func(t *testing.T) {
			observations, err := test.repository.ObserveImportBlobGC(test.ctx, test.digests)
			if !errors.Is(err, attachments.ErrInvalidAttachmentCommand) || observations != nil {
				t.Fatalf("absent snapshot returned observations=%v err=%v", observations, err)
			}
		})
	}
}
