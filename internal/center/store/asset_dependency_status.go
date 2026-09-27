package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetservices"
)

func (r *PostgresAssetServiceRepository) UpdateStatus(ctx context.Context, serviceID string, status assetservices.ServiceStatus, reason string) (assetservices.Record, error) {
	serviceID = strings.TrimSpace(serviceID)
	input := assetservices.NormalizeStatusUpdateInput(assetservices.StatusUpdateInput{Status: status, Reason: reason})
	if serviceID == "" {
		return assetservices.Record{}, fmt.Errorf("%w: service_id is required", assetservices.ErrInvalidServiceInput)
	}
	if err := assetservices.ValidateStatusUpdateInput(input); err != nil {
		return assetservices.Record{}, err
	}
	if r.beginTx == nil {
		return assetservices.Record{}, errors.New("asset service repository cannot correct status without transaction support")
	}

	tx, err := beginAssetGraphTx(ctx, r.beginTx)
	if err != nil {
		return assetservices.Record{}, fmt.Errorf("begin asset service status correction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanAssetService(tx.QueryRow(ctx, `
		select `+assetServiceSelectColumns+`
		from asset_services
		where service_id = $1
		for update`, serviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetservices.Record{}, assetservices.ErrServiceNotFound
	}
	if err != nil {
		return assetservices.Record{}, fmt.Errorf("lock asset service %q: %w", serviceID, err)
	}
	if !assetservices.IsValidServiceStatus(current.Status) {
		return assetservices.Record{}, fmt.Errorf("%w: existing status is unsupported", assetservices.ErrServiceStatusConflict)
	}

	if current.Status == input.Status {
		if err := tx.Commit(ctx); err != nil {
			return assetservices.Record{}, fmt.Errorf("commit unchanged asset service status: %w", err)
		}
		return current, nil
	}
	updated, err := scanAssetService(tx.QueryRow(ctx, `
		update asset_services
		set status = $2,
		    updated_at = now()
		where service_id = $1
		returning `+assetServiceSelectColumns,
		serviceID,
		string(input.Status),
	))
	if err != nil {
		if mappedErr := mapAssetServiceWriteError(err); mappedErr != nil {
			return assetservices.Record{}, mappedErr
		}
		return assetservices.Record{}, fmt.Errorf("update asset service status %q: %w", serviceID, err)
	}

	if err := auditAssociatedObjectStatus(ctx, tx, "service", serviceID, string(current.Status), string(updated.Status), input.Reason); err != nil {
		return assetservices.Record{}, fmt.Errorf("record asset service status correction %q: %w", serviceID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetservices.Record{}, fmt.Errorf("commit asset service status correction %q: %w", serviceID, err)
	}
	return updated, nil
}

func (r *PostgresAssetDomainRepository) UpdateStatus(ctx context.Context, domainID string, status assetdomains.DomainStatus, reason string) (assetdomains.Record, error) {
	domainID = strings.TrimSpace(domainID)
	input := assetdomains.NormalizeStatusUpdateInput(assetdomains.StatusUpdateInput{Status: status, Reason: reason})
	if domainID == "" {
		return assetdomains.Record{}, fmt.Errorf("%w: domain_id is required", assetdomains.ErrInvalidDomainInput)
	}
	if err := assetdomains.ValidateStatusUpdateInput(input); err != nil {
		return assetdomains.Record{}, err
	}
	if r.beginTx == nil {
		return assetdomains.Record{}, errors.New("asset domain repository cannot correct status without transaction support")
	}

	tx, err := beginAssetGraphTx(ctx, r.beginTx)
	if err != nil {
		return assetdomains.Record{}, fmt.Errorf("begin asset domain status correction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanAssetDomain(tx.QueryRow(ctx, `
		select `+assetDomainSelectColumns+`
		from asset_domains
		where domain_id = $1
		for update`, domainID))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetdomains.Record{}, assetdomains.ErrDomainNotFound
	}
	if err != nil {
		return assetdomains.Record{}, fmt.Errorf("lock asset domain %q: %w", domainID, err)
	}
	if !assetdomains.IsValidDomainStatus(current.Status) {
		return assetdomains.Record{}, fmt.Errorf("%w: existing status is unsupported", assetdomains.ErrDomainStatusConflict)
	}

	if current.Status == input.Status {
		if err := tx.Commit(ctx); err != nil {
			return assetdomains.Record{}, fmt.Errorf("commit unchanged asset domain status: %w", err)
		}
		return current, nil
	}
	updated, err := scanAssetDomain(tx.QueryRow(ctx, `
		update asset_domains
		set status = $2,
		    updated_at = now()
		where domain_id = $1
		returning `+assetDomainSelectColumns,
		domainID,
		string(input.Status),
	))
	if err != nil {
		if mappedErr := mapAssetDomainWriteError(err); mappedErr != nil {
			return assetdomains.Record{}, mappedErr
		}
		return assetdomains.Record{}, fmt.Errorf("update asset domain status %q: %w", domainID, err)
	}

	if err := auditAssociatedObjectStatus(ctx, tx, "domain", domainID, string(current.Status), string(updated.Status), input.Reason); err != nil {
		return assetdomains.Record{}, fmt.Errorf("record asset domain status correction %q: %w", domainID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdomains.Record{}, fmt.Errorf("commit asset domain status correction %q: %w", domainID, err)
	}
	return updated, nil
}

func auditAssociatedObjectStatus(ctx context.Context, tx pgx.Tx, kind, objectID, before, after, reason string) error {
	table, _, idColumn, err := relationTables(kind)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `select distinct vps_id from `+table+` where `+idColumn+`=$1 order by vps_id`, objectID)
	if err != nil {
		return err
	}
	var vpsIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		vpsIDs = append(vpsIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, vpsID := range vpsIDs {
		action, err := insertAssetLifecycleAudit(ctx, tx, vpsID, assetlifecycle.ActionTypeCorrectDependencyStatus, assetlifecycle.ActionStatusCompleted, reason, map[string]any{"object_type": kind, "object_id": objectID, "before_status": before, "after_status": after})
		if err != nil {
			return err
		}
		if _, err = insertLifecycleStep(ctx, tx, action.ActionID, kind, objectID, "dependency_status", assetlifecycle.StepStatusCompleted, map[string]any{"status": before}, map[string]any{"status": after}, reason); err != nil {
			return err
		}
	}
	return nil
}
