//go:build linux

package deploy

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/store"
)

type ordinaryNamespaceMounts struct {
	backend     string
	blobRoot    string
	identityDir string
}

// Resolve the same bind mounts and backend the selected namespace one-shot
// will receive. Compose has already normalized bind sources to host paths.
func ordinaryNamespaceMountsFromRecipe(recipe ordinaryNamespaceRecipe) (ordinaryNamespaceMounts, error) {
	var result ordinaryNamespaceMounts
	backend, present := recipe.Environment["HOUFENG_ATTACHMENT_BLOB_BACKEND"]
	if !present || backend == nil || (*backend != "local" && *backend != "s3") {
		return result, errOrdinaryUpgrade
	}
	result.backend = *backend
	if result.backend == "s3" {
		return result, nil
	}
	for _, mount := range recipe.Volumes {
		if mount.Target != composeNamespaceRoot && mount.Target != composeNamespaceIdentityDir {
			continue
		}
		if mount.Type != "bind" || mount.ReadOnly || !filepath.IsAbs(mount.Source) || filepath.Clean(mount.Source) != mount.Source || mount.Source == "/" {
			return ordinaryNamespaceMounts{}, errOrdinaryUpgrade
		}
		if mount.Target == composeNamespaceRoot {
			if result.blobRoot != "" {
				return ordinaryNamespaceMounts{}, errOrdinaryUpgrade
			}
			result.blobRoot = mount.Source
		} else {
			if result.identityDir != "" {
				return ordinaryNamespaceMounts{}, errOrdinaryUpgrade
			}
			result.identityDir = mount.Source
		}
	}
	if result.blobRoot == "" || result.identityDir == "" || fenceOverlap(result.blobRoot, result.identityDir) {
		return ordinaryNamespaceMounts{}, errOrdinaryUpgrade
	}
	return result, nil
}

type ordinaryNamespaceQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// The host observer checks for existing local blob references before ACTIVE;
// the fenced controller repeats the same check before starting the participant.
func ordinaryNamespaceNoLocalReferences(ctx context.Context, db ordinaryNamespaceQueryer, c81 bool) error {
	var present bool
	if err := db.QueryRow(ctx, `select
		exists(select 1 from public.blob_objects where backend_kind = 'local') or
		exists(select 1 from public.blob_gc_deletions where backend_kind = 'local') or
		exists(select 1 from public.blob_publication_intents where backend_kind = 'local') or
		exists(select 1 from public.attachment_uploads u where u.transport_kind = 'local' and u.temporary_object_key is not null) or
		exists(select 1 from public.attachment_upload_parts p join public.attachment_uploads u using (upload_id) where u.transport_kind = 'local')`).Scan(&present); err != nil || present {
		return errOrdinaryUpgrade
	}
	if c81 {
		if err := db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations where backend_kind = 'local')`).Scan(&present); err != nil || present {
			return errOrdinaryUpgrade
		}
	}
	return nil
}

// The DB stores the container path; physical checks must use the selected
// initializer's bind source on the host. No data or identity is modified.
func ordinaryNamespacePreflight(ctx context.Context, recipe ordinaryNamespaceRecipe, observer *pgx.Conn, physical ordinaryPhysicalDatabase, class string) error {
	mounts, err := ordinaryNamespaceMountsFromRecipe(recipe)
	if err != nil || observer == nil {
		return errOrdinaryUpgrade
	}
	if mounts.backend == "s3" {
		return nil
	}
	var tableExists, obligationsExist bool
	if err := observer.QueryRow(ctx, `select to_regclass('public.local_blob_namespaces') is not null, to_regclass('public.record_write_obligations') is not null`).Scan(&tableExists, &obligationsExist); err != nil || tableExists != obligationsExist || (class == ordinaryPredecessorC80 && tableExists) || (class == ordinaryPredecessorC81 && !tableExists) {
		return errOrdinaryUpgrade
	}
	hasBinding := func(ctx context.Context, _ string) (bool, error) {
		if err := ordinaryNamespaceNoLocalReferences(ctx, observer, tableExists); err != nil {
			return true, err
		}
		if !tableExists {
			return false, nil
		}
		var exists bool
		err := observer.QueryRow(ctx, `select exists(select 1 from public.local_blob_namespaces)`).Scan(&exists)
		return exists, err
	}
	dir, err := unix.Openat2(unix.AT_FDCWD, mounts.identityDir, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if errors.Is(err, unix.ENOENT) {
		if !attachments.ColdReleasedLocalNamespaceRoot(mounts.blobRoot, composeNamespaceUID, composeNamespaceGID) {
			return errOrdinaryUpgrade
		}
		return composeNamespaceRequireFresh(ctx, mounts.blobRoot, composeNamespaceUID, composeNamespaceGID, hasBinding)
	}
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(dir)
	if !composeNamespaceStat(dir, unix.S_IFDIR, composeNamespaceUID, composeNamespaceGID, 0o700, false) {
		return errOrdinaryUpgrade
	}
	lock, err := unix.Openat2(dir, "bootstrap.lock", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err == nil {
		if !composeNamespaceStat(lock, unix.S_IFREG, composeNamespaceUID, composeNamespaceGID, 0o600, true) {
			unix.Close(lock)
			return errOrdinaryUpgrade
		}
		unix.Close(lock)
	} else if !errors.Is(err, unix.ENOENT) {
		return errOrdinaryUpgrade
	}
	identity, absent, err := readComposeNamespaceIdentity(dir, composeNamespaceUID, composeNamespaceGID)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if absent {
		if !attachments.ColdReleasedLocalNamespaceRoot(mounts.blobRoot, composeNamespaceUID, composeNamespaceGID) {
			return errOrdinaryUpgrade
		}
		return composeNamespaceRequireFresh(ctx, mounts.blobRoot, composeNamespaceUID, composeNamespaceGID, hasBinding)
	}
	var existing *attachments.LocalNamespace
	if tableExists {
		config, err := pgxpool.ParseConfig("postgres://fixture:fixture@127.0.0.1:5432/fixture?sslmode=disable")
		if err != nil {
			return errOrdinaryUpgrade
		}
		config.ConnConfig = observer.Config().Copy()
		config.MaxConns = 1
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			return errOrdinaryUpgrade
		}
		defer pool.Close()
		var systemID string
		var oid uint32
		var started time.Time
		if err := pool.QueryRow(ctx, `select (select system_identifier::text from pg_catalog.pg_control_system()),(select oid from pg_catalog.pg_database where datname=current_database()),pg_postmaster_start_time()`).Scan(&systemID, &oid, &started); err != nil || systemID != physical.SystemIdentifier || oid != physical.DatabaseOID || !started.Equal(physical.PostmasterStarted) {
			return errOrdinaryUpgrade
		}
		repository := store.NewPostgresLocalBlobRepository(pool, nil, nil)
		value, err := repository.LoadLocalNamespace(ctx, composeNamespaceRoot)
		if err == nil {
			value.RootPath = mounts.blobRoot
			existing = &value
		} else if !errors.Is(err, attachments.ErrBlobNotFound) {
			return errOrdinaryUpgrade
		}
	}
	if existing == nil {
		return errOrdinaryNamespaceReplay
	}
	request := attachments.LocalProvisionRequest{Root: mounts.blobRoot, NamespaceID: identity.NamespaceID, IncarnationID: identity.IncarnationID, RequestID: identity.RequestID, OwnerUID: composeNamespaceUID, OwnerGID: composeNamespaceGID}
	return attachments.PreflightLocalNamespace(ctx, request, existing)
}
