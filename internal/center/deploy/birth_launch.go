package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/sourceretirement"
)

const (
	composeCenterBirthLaunchFile    = "birth-launch.json"
	composeProcessorBirthLaunchFile = "birth-launch-processor.json"
	composeCenterExecutable         = "/usr/local/bin/houfeng-center"
	composeProcessorExecutable      = "/usr/local/bin/houfeng-content-processor"
)

// composeExecutablePaths binds both launch identities to one explicit image.
// Cold callers must validate the paths against the pinned image.
type composeExecutablePaths struct {
	center    string
	processor string
}

func normalComposeExecutablePaths() composeExecutablePaths {
	return composeExecutablePaths{center: composeCenterExecutable, processor: composeProcessorExecutable}
}

var errComposeBirthLaunch = errors.New("Compose birth launch verification failed")

type composeBirthLaunchPair struct {
	revision        int64
	center          recordbirth.LaunchBinding
	processor       recordbirth.LaunchBinding
	centerBytes     []byte
	processorBytes  []byte
	centerDigest    [32]byte
	processorDigest [32]byte
}

type composeBirthLaunchRegistry struct {
	revision       int64
	opened         bool
	runtimeRole    string
	workerRole     string
	centerBytes    []byte
	processorBytes []byte
}

// The normal initializer may create only the first ordinary registration. A
// closed installed registration, or any replacement, needs an offline ceremony.
func prepareComposeBirthLaunch(ctx context.Context, owner *pgxpool.Pool, state recordauthority.VerifiedComposeState, maintenance *SourceMaintenanceConfig, directory, runtimeRole, workerRole string) (composeBirthLaunchPair, error) {
	paths := normalComposeExecutablePaths()
	pair, err := expectedComposeBirthLaunch(ctx, owner, state, maintenance, paths)
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	registry, err := readComposeBirthLaunchRegistry(ctx, owner)
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	if registry == nil {
		if pair.center.BirthInstalled {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
		var revision int64
		if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`,
			pair.centerBytes, pair.processorBytes, runtimeRole, workerRole, int64(0)).Scan(&revision); err != nil || revision != 1 {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
		registry = &composeBirthLaunchRegistry{revision: revision, runtimeRole: runtimeRole, workerRole: workerRole,
			centerBytes: pair.centerBytes, processorBytes: pair.processorBytes}
	} else if registry.runtimeRole != runtimeRole || registry.workerRole != workerRole ||
		!bytes.Equal(registry.centerBytes, pair.centerBytes) || !bytes.Equal(registry.processorBytes, pair.processorBytes) ||
		(pair.center.BirthInstalled && !registry.opened) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}

	if !registry.opened {
		if err := publishComposeBirthLaunchPair(directory, pair); err != nil {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
	}
	observed, err := inspectPublishedComposeBirthLaunch(ctx, owner, state, maintenance, directory, registry.opened, paths)
	if err != nil && !registry.opened {
		// Another initializer can finish the same staged pair before this
		// readback. Accept only its exact opened revision and role binding.
		observed, err = inspectPublishedComposeBirthLaunch(ctx, owner, state, maintenance, directory, true, paths)
		if err == nil {
			registry.opened = true
		}
	}
	if err != nil || observed.revision != registry.revision || !bytes.Equal(observed.centerBytes, pair.centerBytes) || !bytes.Equal(observed.processorBytes, pair.processorBytes) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	if !registry.opened {
		if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, registry.revision); err != nil {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
		observed, err = inspectPublishedComposeBirthLaunch(ctx, owner, state, maintenance, directory, true, paths)
		if err != nil || observed.revision != registry.revision || !bytes.Equal(observed.centerBytes, pair.centerBytes) || !bytes.Equal(observed.processorBytes, pair.processorBytes) {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
	}
	current, err := readComposeBirthLaunchRegistry(ctx, owner)
	if err != nil || current == nil || current.revision != registry.revision || !current.opened ||
		current.runtimeRole != runtimeRole || current.workerRole != workerRole ||
		!bytes.Equal(current.centerBytes, pair.centerBytes) || !bytes.Equal(current.processorBytes, pair.processorBytes) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	return observed, nil
}

func readComposeBirthLaunchRegistry(ctx context.Context, owner *pgxpool.Pool) (*composeBirthLaunchRegistry, error) {
	if owner == nil {
		return nil, errComposeBirthLaunch
	}
	var value composeBirthLaunchRegistry
	err := owner.QueryRow(ctx, `select revision,opened,runtime_principal::text,worker_principal::text,center_binding,processor_binding
		from record_platform_internal.record_birth_launch_state where singleton`).Scan(
		&value.revision, &value.opened, &value.runtimeRole, &value.workerRole, &value.centerBytes, &value.processorBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || value.revision <= 0 {
		return nil, errComposeBirthLaunch
	}
	return &value, nil
}

// Only the database owner reads private birth tables. Runtime admission is not
// usable here: the launch registry has not necessarily been opened yet.
func expectedComposeBirthLaunch(ctx context.Context, owner *pgxpool.Pool, state recordauthority.VerifiedComposeState, maintenance *SourceMaintenanceConfig, paths composeExecutablePaths) (composeBirthLaunchPair, error) {
	if owner == nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	source, err := readSourceAdmission(ctx, owner)
	if err != nil || !composeBirthLaunchSourceMatches(source, maintenance, state) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	var installed bool
	if err := owner.QueryRow(ctx, `select exists(select 1 from record_platform_internal.record_birth_lineage where singleton)`).Scan(&installed); err != nil || installed != (maintenance != nil && maintenance.BirthInstalled) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	var binding recordbirth.Binding
	if installed {
		rows, err := owner.Query(ctx, `select b.authority_id,b.lineage_id,b.deployment_id,b.project_id,b.generation,b.key_id,b.cutover_digest
			from record_platform_internal.record_birth_bindings b
			where b.generation=$1 and b.deployment_id=$2 and b.authority_id=$3
			and (exists(select 1 from record_platform_internal.record_birth_enabled e where e.singleton
				and (e.generation,e.key_id,e.cutover_digest)=(b.generation,b.key_id,b.cutover_digest))
				or (not exists(select 1 from record_platform_internal.record_birth_enabled)
					and (select count(*) from record_platform_internal.record_birth_bindings c where c.generation=b.generation)=1))`,
			source.Generation, source.DeploymentID, source.Witness.ID)
		if err != nil {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
		count := 0
		for rows.Next() {
			count++
			var generation int64
			var cutover []byte
			if err := rows.Scan(&binding.AuthorityID, &binding.LineageID, &binding.DeploymentID, &binding.ProjectID, &generation, &binding.KeyID, &cutover); err != nil || generation <= 0 || len(cutover) != len(binding.CutoverDigest) {
				rows.Close()
				return composeBirthLaunchPair{}, errComposeBirthLaunch
			}
			binding.Generation = uint64(generation)
			copy(binding.CutoverDigest[:], cutover)
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil || count != 1 || binding.Validate() != nil || binding.LineageID != maintenance.BirthLineageID ||
			maintenance.BirthProtocolVersion != 1 || binding.AuthorityID != maintenance.Witness.ID ||
			binding.DeploymentID != string(state.DeploymentID) || binding.Generation != uint64(source.Generation) {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
	}
	centerDigest, err := recordbirth.DigestExecutable(paths.center)
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	processorDigest, err := recordbirth.DigestExecutable(paths.processor)
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	center := recordbirth.LaunchBinding{
		ProtocolVersion: 1, ProcessKind: "center", DeploymentID: string(state.DeploymentID),
		BirthInstalled: installed, BirthBinding: binding,
		ActivationBundleDigest: state.ActivationCommand.ActivationBundleDigest, BinaryDigest: centerDigest,
	}
	if source != nil {
		center.SourceGeneration = uint64(source.Generation)
		center.Witness = source.Witness
	}
	processor := center
	processor.ProcessKind = "content-processor"
	processor.BinaryDigest = processorDigest
	return encodeComposeBirthLaunchPair(center, processor)
}

func composeBirthLaunchSourceMatches(source *sourceAdmission, maintenance *SourceMaintenanceConfig, state recordauthority.VerifiedComposeState) bool {
	if maintenance == nil {
		return source == nil
	}
	return source != nil && source.Mode == "active" && source.Generation > 0 &&
		source.DeploymentID == string(state.DeploymentID) && source.Witness == maintenance.Witness
}

func encodeComposeBirthLaunchPair(center, processor recordbirth.LaunchBinding) (composeBirthLaunchPair, error) {
	centerBytes, err := recordbirth.EncodeLaunchBinding(center)
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	processorBytes, err := recordbirth.EncodeLaunchBinding(processor)
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	return composeBirthLaunchPair{center: center, processor: processor, centerBytes: centerBytes, processorBytes: processorBytes,
		centerDigest: sha256.Sum256(centerBytes), processorDigest: sha256.Sum256(processorBytes)}, nil
}

// Every invocation reads both protected files, the fixed images and, when
// installed, a fresh independent W2 anchor before the APP ancestry inspection.
func inspectPublishedComposeBirthLaunch(ctx context.Context, app *pgxpool.Pool, state recordauthority.VerifiedComposeState, maintenance *SourceMaintenanceConfig, directory string, opened bool, paths composeExecutablePaths) (composeBirthLaunchPair, error) {
	centerPath := filepath.Join(directory, composeCenterBirthLaunchFile)
	processorPath := filepath.Join(directory, composeProcessorBirthLaunchFile)
	center, centerDigest, err := recordbirth.ReadPublishedLaunchBinding(centerPath, "center")
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	processor, processorDigest, err := recordbirth.ReadPublishedLaunchBinding(processorPath, "content-processor")
	if err != nil {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	pair, err := encodeComposeBirthLaunchPair(center, processor)
	if err != nil || pair.centerDigest != centerDigest || pair.processorDigest != processorDigest {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	other := processor
	other.ProcessKind, other.BinaryDigest = center.ProcessKind, center.BinaryDigest
	if other != center || center.DeploymentID != string(state.DeploymentID) ||
		center.ActivationBundleDigest != state.ActivationCommand.ActivationBundleDigest {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	source, err := readSourceAdmission(ctx, app)
	if err != nil || !composeBirthLaunchSourceMatches(source, maintenance, state) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	if source == nil {
		if center.SourceGeneration != 0 || center.Witness != (sourceretirement.DomainIdentity{}) || center.BirthInstalled {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
	} else if center.SourceGeneration != uint64(source.Generation) || center.Witness != source.Witness ||
		center.BirthInstalled != maintenance.BirthInstalled ||
		(center.BirthInstalled && (maintenance.BirthProtocolVersion != 1 || center.BirthBinding.LineageID != maintenance.BirthLineageID)) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	centerImage, err := recordbirth.DigestExecutable(paths.center)
	if err != nil || centerImage != center.BinaryDigest {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	processorImage, err := recordbirth.DigestExecutable(paths.processor)
	if err != nil || processorImage != processor.BinaryDigest {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	var anchorSequence, anchorHash any
	if center.BirthInstalled {
		observation, err := observeComposeBirthContinuity(ctx, app, maintenance, center.BirthBinding)
		if err != nil || observation.Anchor.Sequence > math.MaxInt64 {
			return composeBirthLaunchPair{}, errComposeBirthLaunch
		}
		anchorSequence, anchorHash = int64(observation.Anchor.Sequence), observation.Anchor.Hash[:]
	}
	var revision int64
	var actualOpened bool
	var actualCenter, actualProcessor []byte
	if err := app.QueryRow(ctx, `select revision,opened,center_binding,processor_binding from public.record_birth_launch_inspect($1::bigint,$2::bytea)`,
		anchorSequence, anchorHash).Scan(&revision, &actualOpened, &actualCenter, &actualProcessor); err != nil || revision <= 0 || actualOpened != opened ||
		!bytes.Equal(actualCenter, pair.centerBytes) || !bytes.Equal(actualProcessor, pair.processorBytes) {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	pair.revision = revision
	// Reopen both names after the external observation and APP readback.
	_, centerAgain, err := recordbirth.ReadPublishedLaunchBinding(centerPath, "center")
	if err != nil || centerAgain != centerDigest {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	_, processorAgain, err := recordbirth.ReadPublishedLaunchBinding(processorPath, "content-processor")
	if err != nil || processorAgain != processorDigest {
		return composeBirthLaunchPair{}, errComposeBirthLaunch
	}
	return pair, nil
}

func openComposeBirthContinuityReader(ctx context.Context, app *pgxpool.Pool, maintenance *SourceMaintenanceConfig) (*pgxpool.Pool, *recordbirth.PostgresAuthority, error) {
	if maintenance == nil || !maintenance.BirthInstalled || maintenance.BirthReaderDatabaseURL == "" {
		return nil, nil, errComposeBirthLaunch
	}
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(maintenance.BirthReaderDatabaseURL)
	if err != nil {
		return nil, nil, errComposeBirthLaunch
	}
	readerPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		if readerPool != nil {
			readerPool.Close()
		}
		return nil, nil, errComposeBirthLaunch
	}
	reader, err := recordbirth.NewPostgresContinuityReader(readerPool, app, maintenance.Witness)
	if err != nil {
		readerPool.Close()
		return nil, nil, errComposeBirthLaunch
	}
	return readerPool, reader, nil
}

func observeComposeBirthContinuity(ctx context.Context, app *pgxpool.Pool, maintenance *SourceMaintenanceConfig, binding recordbirth.Binding) (recordbirth.ContinuityObservation, error) {
	readerPool, reader, err := openComposeBirthContinuityReader(ctx, app, maintenance)
	if err != nil {
		return recordbirth.ContinuityObservation{}, errComposeBirthLaunch
	}
	defer readerPool.Close()
	observation, err := reader.Continuity(ctx, binding)
	if err != nil || observation.Binding != binding || len(observation.Proof) == 0 {
		return recordbirth.ContinuityObservation{}, fmt.Errorf("%w: continuity unavailable", errComposeBirthLaunch)
	}
	return observation, nil
}
