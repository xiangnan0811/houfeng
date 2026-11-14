package main

import (
	"context"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/sourceretirement"
)

var errSourceAuthorityConfiguration = sourceretirement.ErrAuthorityConfiguration

// Once enrolled, omitting environment configuration must not reopen the legacy
// live-source path. The APP row survives ordinary restarts; recovery explicitly
// closes it before restored credentials or listeners can become available.
func configuredSourceAuthority(local, witness *pgxpool.Pool) (*sourceretirement.Authority, error) {
	identityPath := os.Getenv("HOUFENG_SOURCE_WITNESS_IDENTITY_FILE")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return sourceretirement.OpenConfiguredAuthority(ctx, local, witness, identityPath)
}
