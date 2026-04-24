package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/observations"
)

const selectProbeMetadataSQL = `
	select target_id, probe_kind
	from probe_items
	where probe_item_id = $1`

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getProbeMetadata(ctx context.Context, queryer queryRower, probeItemID string) (observations.ProbeMetadata, error) {
	var metadata observations.ProbeMetadata
	if err := queryer.QueryRow(ctx, selectProbeMetadataSQL, probeItemID).Scan(&metadata.TargetID, &metadata.ProbeKind); errors.Is(err, pgx.ErrNoRows) {
		return observations.ProbeMetadata{}, observations.ErrProbeMetadataNotFound
	} else if err != nil {
		return observations.ProbeMetadata{}, fmt.Errorf("query probe metadata %q: %w", probeItemID, err)
	}

	return metadata, nil
}
