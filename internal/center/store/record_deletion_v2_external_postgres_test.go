package store_test

import (
	"testing"

	"houfeng/internal/center/portability"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/store"
)

func TestPostgresIntegrationRecordDeletionV2FinishResponsibilities(t *testing.T) {
	store.RunV2FinishResponsibilities(t, func(repository *store.PostgresRecordDeletionRepository) (recorddeletion.Adapter, error) {
		return portability.NewDeletionAdapter(repository)
	})
}

func TestRecordDeletionV2PreparedLocks(t *testing.T) {
	store.RunV2PreparedLocks(t, func(repository *store.PostgresRecordDeletionRepository) (recorddeletion.Adapter, error) {
		return portability.NewDeletionAdapter(repository)
	})
}

func TestPostgresIntegrationRecordDeletionV2Issuer(t *testing.T) {
	store.RunV2Issuer(t, func(repository *store.PostgresRecordDeletionRepository) (recorddeletion.Adapter, error) {
		return portability.NewDeletionAdapter(repository)
	})
}
