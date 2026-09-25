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
	"houfeng/internal/center/vpsassets"
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

	var vpsID string
	var lifecycle vpsassets.LifecycleStatus
	if err := tx.QueryRow(ctx, `
		select v.vps_id, v.lifecycle_status
		from asset_services s
		join vps_assets v on v.vps_id = s.vps_id
		where s.service_id = $1
		for update of v`, serviceID).Scan(&vpsID, &lifecycle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return assetservices.Record{}, assetservices.ErrServiceNotFound
		}
		return assetservices.Record{}, fmt.Errorf("lock asset service owner %q: %w", serviceID, err)
	}

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
	if current.VPSID != vpsID {
		return assetservices.Record{}, assetservices.ErrServiceStatusConflict
	}
	if !assetservices.IsValidServiceStatus(current.Status) {
		return assetservices.Record{}, fmt.Errorf("%w: existing status is unsupported", assetservices.ErrServiceStatusConflict)
	}

	var targetStatus string
	if current.TargetID != nil {
		targetStatus, err = lockAssetTargetRunStatus(ctx, tx, *current.TargetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetservices.Record{}, assetservices.ErrServiceTargetNotFound
		}
		if err != nil {
			return assetservices.Record{}, fmt.Errorf("lock target %q for asset service status correction: %w", *current.TargetID, err)
		}
	}

	if current.Status == input.Status {
		if err := tx.Commit(ctx); err != nil {
			return assetservices.Record{}, fmt.Errorf("commit unchanged asset service status: %w", err)
		}
		return current, nil
	}
	if input.Status == assetservices.ServiceStatusActive {
		if isTerminalVPSLifecycle(lifecycle) {
			return assetservices.Record{}, fmt.Errorf("%w: terminal %s VPS cannot activate an asset service", vpsassets.ErrVPSAssetReadonly, lifecycle)
		}
		if current.TargetID != nil {
			if err := ensureTargetAllowsAssetRelationship(*current.TargetID, targetStatus, string(input.Status), "service"); err != nil {
				return assetservices.Record{}, err
			}
		}
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

	action, err := insertAssetLifecycleAudit(
		ctx,
		tx,
		vpsID,
		assetlifecycle.ActionTypeCorrectDependencyStatus,
		assetlifecycle.ActionStatusCompleted,
		input.Reason,
		map[string]any{
			"object_type":   "service",
			"object_id":     serviceID,
			"before_status": string(current.Status),
			"after_status":  string(updated.Status),
		},
	)
	if err != nil {
		return assetservices.Record{}, fmt.Errorf("audit asset service status correction %q: %w", serviceID, err)
	}
	if _, err := insertLifecycleStep(
		ctx,
		tx,
		action.ActionID,
		"service",
		serviceID,
		"dependency_status",
		assetlifecycle.StepStatusCompleted,
		map[string]any{"status": string(current.Status)},
		map[string]any{"status": string(updated.Status)},
		input.Reason,
	); err != nil {
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

	var vpsID string
	var lifecycle vpsassets.LifecycleStatus
	if err := tx.QueryRow(ctx, `
		select v.vps_id, v.lifecycle_status
		from asset_domains d
		join vps_assets v on v.vps_id = d.vps_id
		where d.domain_id = $1
		for update of v`, domainID).Scan(&vpsID, &lifecycle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return assetdomains.Record{}, assetdomains.ErrDomainNotFound
		}
		return assetdomains.Record{}, fmt.Errorf("lock asset domain owner %q: %w", domainID, err)
	}

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
	if current.VPSID != vpsID {
		return assetdomains.Record{}, assetdomains.ErrDomainStatusConflict
	}
	if !assetdomains.IsValidDomainStatus(current.Status) {
		return assetdomains.Record{}, fmt.Errorf("%w: existing status is unsupported", assetdomains.ErrDomainStatusConflict)
	}

	var targetStatus string
	if current.TargetID != nil {
		targetStatus, err = lockAssetTargetRunStatus(ctx, tx, *current.TargetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetdomains.Record{}, assetdomains.ErrDomainTargetNotFound
		}
		if err != nil {
			return assetdomains.Record{}, fmt.Errorf("lock target %q for asset domain status correction: %w", *current.TargetID, err)
		}
	}

	if current.Status == input.Status {
		if err := tx.Commit(ctx); err != nil {
			return assetdomains.Record{}, fmt.Errorf("commit unchanged asset domain status: %w", err)
		}
		return current, nil
	}
	if input.Status == assetdomains.DomainStatusActive {
		if isTerminalVPSLifecycle(lifecycle) {
			return assetdomains.Record{}, fmt.Errorf("%w: terminal %s VPS cannot activate an asset domain", vpsassets.ErrVPSAssetReadonly, lifecycle)
		}
		if current.TargetID != nil {
			if err := ensureTargetAllowsAssetRelationship(*current.TargetID, targetStatus, string(input.Status), "domain"); err != nil {
				return assetdomains.Record{}, err
			}
		}
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

	action, err := insertAssetLifecycleAudit(
		ctx,
		tx,
		vpsID,
		assetlifecycle.ActionTypeCorrectDependencyStatus,
		assetlifecycle.ActionStatusCompleted,
		input.Reason,
		map[string]any{
			"object_type":   "domain",
			"object_id":     domainID,
			"before_status": string(current.Status),
			"after_status":  string(updated.Status),
		},
	)
	if err != nil {
		return assetdomains.Record{}, fmt.Errorf("audit asset domain status correction %q: %w", domainID, err)
	}
	if _, err := insertLifecycleStep(
		ctx,
		tx,
		action.ActionID,
		"domain",
		domainID,
		"dependency_status",
		assetlifecycle.StepStatusCompleted,
		map[string]any{"status": string(current.Status)},
		map[string]any{"status": string(updated.Status)},
		input.Reason,
	); err != nil {
		return assetdomains.Record{}, fmt.Errorf("record asset domain status correction %q: %w", domainID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdomains.Record{}, fmt.Errorf("commit asset domain status correction %q: %w", domainID, err)
	}
	return updated, nil
}
