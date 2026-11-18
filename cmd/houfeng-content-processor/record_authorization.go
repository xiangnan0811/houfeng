package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store"
)

func newProcessorAttachmentRepository(ctx context.Context, config contentProcessorConfig, pool *pgxpool.Pool) (attachments.ProcessorRepository, func(), error) {
	if ctx == nil || pool == nil {
		return nil, nil, errors.New("processor authorization requires an APP database")
	}
	gate, err := processorRecordAdmissionGate(config)
	if err != nil {
		return nil, nil, err
	}
	var witness *pgxpool.Pool
	if config.SourceWitnessDatabaseURL != "" {
		witness, err = pgxpool.New(ctx, config.SourceWitnessDatabaseURL)
		if err != nil {
			return nil, nil, errors.New("open processor source witness")
		}
	}
	closeWitness := func() {
		if witness != nil {
			witness.Close()
		}
	}
	configured := false
	defer func() {
		if !configured {
			closeWitness()
		}
	}()
	setupContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var authority *sourceretirement.Authority
	if witness != nil || config.SourceWitnessIdentityFile != "" {
		authority, err = sourceretirement.OpenConfiguredAuthority(setupContext, pool, witness, config.SourceWitnessIdentityFile)
		if err != nil {
			return nil, nil, err
		}
	}
	// With no configured witness, this reader checks APP enrollment on each
	// source authorization and fails closed for enrolled sources. Standalone
	// processing and existing cleanup do not gain a dependency on W2.
	tombstones, err := store.NewWitnessedRecordSubjectTombstoneReader(
		recordplatform.DeploymentID(config.RecordDeploymentID), recordauth.ProjectIDDefault, authority, pool,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create processor source witness reader: %w", err)
	}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(store.NewPostgresVPSAssetRepository(pool)),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(pool)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(pool)),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create processor source registry: %w", err)
	}
	resolver := store.NewRecordSubjectReadResolver(subjects, tombstones)
	authorizations := store.NewPostgresCurrentRecordAuthorizationSource(pool, resolver, gate)
	repository := store.NewPostgresAttachmentRepository(pool)
	if err := repository.ConfigureProcessorAdmission(gate); err != nil {
		return nil, nil, fmt.Errorf("configure processor admission: %w", err)
	}
	if err := repository.ConfigureRecordMutationAuthorization(store.NewPostgresCollaborationMembershipReader(), authorizations); err != nil {
		return nil, nil, fmt.Errorf("configure processor mutation authorization: %w", err)
	}
	configured = true
	return repository, closeWitness, nil
}

func processorRecordAdmissionGate(config contentProcessorConfig) (*store.DeploymentMembershipAdmissionGate, error) {
	if config.RecordDeploymentID == "" || config.RecordInstanceID == "" ||
		config.RecordInstanceKind != "worker" || config.RecordInstanceCapability == "" {
		return nil, errors.New("processor record admission requires a complete worker identity")
	}
	return store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID:   config.RecordInstanceID,
		DeploymentID: recordplatform.DeploymentID(config.RecordDeploymentID),
		ProjectID:    recordplatform.ProjectIDDefault,
		InstanceKind: config.RecordInstanceKind,
		Capability:   config.RecordInstanceCapability,
	})
}
