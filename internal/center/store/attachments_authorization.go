package store

import (
	"context"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// AuthorizeRecordAttachmentRead adapts the canonical record authorization
// evidence loader to the attachment content service. The attachment layer
// never reconstructs visibility or source intersections itself.
func (source *PostgresCurrentRecordAuthorizationSource) AuthorizeRecordAttachmentRead(
	ctx context.Context,
	actor recordauth.ActorScope,
	recordID string,
) error {
	if source == nil {
		return ErrRecordSubjectUnavailable
	}
	current, err := source.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), recordID)
	if err != nil {
		return err
	}
	return records.AuthorizeRecordResource(
		actor.Clone(), recordauth.CapabilityAttachmentRead, current.Evidence,
	)
}

var _ attachments.RecordDownloadAuthorizer = (*PostgresCurrentRecordAuthorizationSource)(nil)
