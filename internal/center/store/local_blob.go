package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordbirth"
)

// PostgresLocalBlobRepository uses the caller's direct runtime/admin/worker
// pool. SQL EXECUTE privileges and constrained commands retain each purpose's
// authority; this repository never changes role or keeps admin credentials.
type PostgresLocalBlobRepository struct {
	pool       *pgxpool.Pool
	witness    *WitnessedRecordSubjectTombstoneReader
	continuity recordbirth.ContinuitySource
}

func NewPostgresLocalBlobRepository(pool *pgxpool.Pool, witness *WitnessedRecordSubjectTombstoneReader, continuity recordbirth.ContinuitySource) *PostgresLocalBlobRepository {
	return &PostgresLocalBlobRepository{pool: pool, witness: witness, continuity: continuity}
}

func (repository *PostgresLocalBlobRepository) CheckLocalProvisionAuthority(ctx context.Context) error {
	var allowed bool
	err := repository.pool.QueryRow(ctx, `select current_setting('role')='none' and r.rolcanlogin
		and not r.rolsuper and not r.rolcreaterole and not r.rolcreatedb and not r.rolreplication
		and not r.rolbypassrls and not r.rolinherit
		and not exists(select 1 from pg_auth_members m where m.member=r.oid or m.roleid=r.oid)
		and has_function_privilege(session_user,'public.local_blob_namespace_provision(bytea)','EXECUTE')
		and has_function_privilege(session_user,'public.local_blob_namespace_admit(bytea)','EXECUTE')
		and not exists(select 1 from public.source_authority_admission)
		from pg_roles r where r.rolname=session_user`).Scan(&allowed)
	if err != nil || !allowed {
		return errors.Join(attachments.ErrLocalNamespaceClosed, err)
	}
	return nil
}

// ComposeSourceEnrolled reports source enrollment without granting provisioning authority.
func (repository *PostgresLocalBlobRepository) ComposeSourceEnrolled(ctx context.Context) (bool, error) {
	var enrolled bool
	err := repository.pool.QueryRow(ctx, `select exists(select 1 from public.source_authority_admission)`).Scan(&enrolled)
	return enrolled, err
}

func (repository *PostgresLocalBlobRepository) LoadLocalNamespace(ctx context.Context, root string) (attachments.LocalNamespace, error) {
	var n attachments.LocalNamespace
	err := repository.pool.QueryRow(ctx, `select n.namespace_id::text,n.current_incarnation_id::text,n.provision_request_id::text,
		n.root_path,n.admission_state,n.source_recovery_id,i.boot_id::text,i.filesystem_device,i.root_inode,
		i.digest_inode,i.locks_inode,i.marker_inode,i.owner_uid,i.owner_gid,encode(i.marker_digest,'hex'),i.marker_nonce::text
		from public.local_blob_namespaces n join public.local_blob_namespace_incarnations i
		on i.namespace_id=n.namespace_id and i.incarnation_id=n.current_incarnation_id where n.root_path=$1`, root).Scan(
		&n.NamespaceID, &n.IncarnationID, &n.RequestID, &n.RootPath, &n.AdmissionState, &n.SourceRecoveryID,
		&n.BootID, &n.Device, &n.RootInode, &n.DigestInode, &n.LocksInode, &n.MarkerInode, &n.OwnerUID, &n.OwnerGID, &n.MarkerDigest, &n.MarkerNonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, attachments.ErrBlobNotFound
	}
	return n, err
}

func (repository *PostgresLocalBlobRepository) ProvisionLocalNamespace(ctx context.Context, n attachments.LocalNamespace) (attachments.LocalNamespace, error) {
	body, err := json.Marshal(n)
	if err != nil {
		return attachments.LocalNamespace{}, err
	}
	if _, err := repository.pool.Exec(ctx, `select public.local_blob_namespace_provision($1)`, body); err != nil {
		return attachments.LocalNamespace{}, err
	}
	return repository.LoadLocalNamespace(ctx, n.RootPath)
}

func (repository *PostgresLocalBlobRepository) AdmitLocalNamespace(ctx context.Context, request attachments.LocalNamespaceAdmission) (attachments.LocalNamespace, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return attachments.LocalNamespace{}, err
	}
	var root string
	if err := repository.pool.QueryRow(ctx, `select root_path from public.local_blob_namespace_admit($1)`, body).Scan(&root); err != nil {
		return attachments.LocalNamespace{}, err
	}
	return repository.LoadLocalNamespace(ctx, root)
}

func (repository *PostgresLocalBlobRepository) LoadLocalAnchor(ctx context.Context, namespace, incarnation, key string) (attachments.LocalAnchor, error) {
	var a attachments.LocalAnchor
	err := repository.pool.QueryRow(ctx, `select namespace_id::text,incarnation_id::text,canonical_key,filesystem_device,lock_inode,lock_nonce::text
		from public.local_blob_key_anchors where namespace_id=$1 and incarnation_id=$2 and canonical_key=$3`, namespace, incarnation, key).Scan(
		&a.NamespaceID, &a.IncarnationID, &a.Key, &a.Device, &a.Inode, &a.Nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, attachments.ErrBlobNotFound
	}
	return a, err
}

func (repository *PostgresLocalBlobRepository) RegisterLocalAnchor(ctx context.Context, anchor attachments.LocalAnchor) (attachments.LocalAnchor, error) {
	var a attachments.LocalAnchor
	body, err := json.Marshal(anchor)
	if err != nil {
		return a, err
	}
	err = repository.pool.QueryRow(ctx, `select namespace_id::text,incarnation_id::text,canonical_key,filesystem_device,lock_inode,lock_nonce::text
		from public.local_blob_anchor_register($1)`, body).Scan(&a.NamespaceID, &a.IncarnationID, &a.Key, &a.Device, &a.Inode, &a.Nonce)
	return a, err
}

func (repository *PostgresLocalBlobRepository) RegisterWrite(ctx context.Context, request attachments.WriteRegistration) (attachments.WriteObligation, error) {
	return repository.writeCommand(ctx, `select to_jsonb(o)||jsonb_build_object('sha256',encode(o.sha256_digest,'hex'),'storage_binding',encode(o.storage_binding,'hex')) from public.record_write_register($1) o`, request)
}
func (repository *PostgresLocalBlobRepository) ResolveWrite(ctx context.Context, request attachments.WriteIdentity) (attachments.WriteObligation, error) {
	return repository.writeCommand(ctx, `select to_jsonb(o)||jsonb_build_object('sha256',encode(o.sha256_digest,'hex'),'storage_binding',encode(o.storage_binding,'hex')) from public.record_write_resolve($1) o`, request)
}
func (repository *PostgresLocalBlobRepository) DispatchWrite(ctx context.Context, request attachments.WriteDispatch) (attachments.WriteObligation, error) {
	return repository.writeCommand(ctx, `select to_jsonb(o)||jsonb_build_object('sha256',encode(o.sha256_digest,'hex'),'storage_binding',encode(o.storage_binding,'hex')) from public.record_write_dispatch($1) o`, request)
}
func (repository *PostgresLocalBlobRepository) RecordWriteTemporary(ctx context.Context, request attachments.WriteTemporaryCreation) (attachments.WriteObligation, error) {
	return repository.writeCommand(ctx, `select to_jsonb(o)||jsonb_build_object('sha256',encode(o.sha256_digest,'hex'),'storage_binding',encode(o.storage_binding,'hex')) from public.record_write_temporary_created($1) o`, request)
}
func (repository *PostgresLocalBlobRepository) RecordWriteReturn(ctx context.Context, request attachments.WriteReturn) (attachments.WriteObligation, error) {
	return repository.writeCommand(ctx, `select to_jsonb(o)||jsonb_build_object('sha256',encode(o.sha256_digest,'hex'),'storage_binding',encode(o.storage_binding,'hex')) from public.record_write_return($1) o`, request)
}
func (repository *PostgresLocalBlobRepository) writeCommand(ctx context.Context, statement string, request any) (attachments.WriteObligation, error) {
	var result attachments.WriteObligation
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	var prepared *preparedImportWrite
	checkImport := false
	switch command := request.(type) {
	case attachments.WriteRegistration:
		checkImport = command.Kind == "import_archive" || command.Kind == "import_attachment"
	case attachments.WriteDispatch:
		checkImport = true
	}
	if checkImport {
		replayed, err := managedWriteAlreadyRecorded(ctx, repository.pool, request)
		if err != nil {
			return result, err
		}
		if !replayed {
			prepared, err = repository.prepareImportWrite(ctx, request)
			if err != nil {
				// The original command may have committed while its response or the
				// witness observation was lost. Only its exact SQL replay can settle it.
				replayed, replayErr := managedWriteAlreadyRecorded(ctx, repository.pool, request)
				if replayErr != nil || !replayed {
					return result, errors.Join(err, replayErr)
				}
			}
		}
	}
	run := func(ctx context.Context, tx pgx.Tx, proof []byte) error {
		result = attachments.WriteObligation{}
		if prepared != nil {
			replayed, err := managedWriteAlreadyRecorded(ctx, tx, request)
			if err != nil {
				return err
			}
			if !replayed {
				if err := prepared.acquireAndAuthorize(ctx, tx, request, proof); err != nil {
					return err
				}
			}
		} else if err := prepareManagedWriteMutation(ctx, tx, request); err != nil {
			return err
		}
		var encoded []byte
		if err := tx.QueryRow(ctx, statement, body).Scan(&encoded); err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) && postgresError.Code == "55000" && postgresError.Message == "blob key has active cleanup responsibility" {
				return errors.Join(attachments.ErrBlobGCProtected, err)
			}
			return err
		}
		return json.Unmarshal(encoded, &result)
	}
	if prepared != nil {
		platform := NewPostgresRecordPlatformRepository(repository.pool, AdmissionGateFunc(func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `select public.source_application_admit()`)
			return err
		}))
		err = platform.runRecordBirthMutationTransaction(ctx, repository.continuity, func(ctx context.Context, transaction *RecordPlatformTransaction, proof []byte) error {
			return run(ctx, transaction.tx, proof)
		})
		if err != nil {
			return attachments.WriteObligation{}, err
		}
		return result, nil
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := run(ctx, tx, nil); err != nil {
		return attachments.WriteObligation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.WriteObligation{}, err
	}
	return result, nil
}

// Registration and dispatch can create new physical responsibility. Their
// graph, namespace and comparison fences precede the SQL origin/record locks.
// Readback and late I/O facts keep their separate, gate-independent authority.
func prepareManagedWriteMutation(ctx context.Context, tx pgx.Tx, request any) error {
	var namespace *string
	switch command := request.(type) {
	case attachments.WriteRegistration:
		// Exact registration replay is a read of the existing immutable command,
		// even after its original owner or namespace has closed. SQL still checks
		// the principal and the complete command before returning that fact.
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations where obligation_id=$1)`, command.ObligationID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		namespace = command.NamespaceID
	case attachments.WriteDispatch:
		replayed, err := managedWriteAlreadyRecorded(ctx, tx, command)
		if err != nil || replayed {
			return err
		}
		if err := lockAssetGraphForSync(ctx, tx); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select namespace_id::text from public.record_write_obligations where obligation_id=$1 and writer_instance=$2::uuid`,
			command.ObligationID, command.WriterInstance).Scan(&namespace); err != nil {
			return err
		}
	default:
		return nil
	}
	if _, dispatch := request.(attachments.WriteDispatch); !dispatch {
		if err := lockAssetGraphForSync(ctx, tx); err != nil {
			return err
		}
	}
	if namespace != nil {
		if err := lockLocalBlobNamespaces(ctx, tx, []string{*namespace}); err != nil {
			return err
		}
	}
	return lockComparisonDecisionForWriter(ctx, tx)
}

var _ attachments.LocalNamespaceRepository = (*PostgresLocalBlobRepository)(nil)
var _ attachments.LocalNamespaceAdministrator = (*PostgresLocalBlobRepository)(nil)
var _ attachments.ManagedWriteRepository = (*PostgresLocalBlobRepository)(nil)
