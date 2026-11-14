//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/store/migrate"
)

// Only verifiers are persisted. Each callback receives a new runtime secret;
// after refencing all callback secrets are revoked, including the unused roles.
type ledgerPredecessorRoles struct {
	Version         uint64                          `json:"version"`
	PhaseGeneration uint64                          `json:"phase_generation"`
	RecoveryID      string                          `json:"recovery_id"`
	Application     RecordLedgerApplicationIdentity `json:"application"`
	RealmDigest     string                          `json:"realm_digest"`
	Normal          ledgerAppRoleSnapshot           `json:"normal"`
}

func ledgerPredecessorRuntime(inventory ledgerAppInventory) (string, error) {
	if !inventory.LegacyC80 || len(inventory.Baseline.Manifest.Manifests) == 0 {
		return "", ErrRecordLedgerFence
	}
	latest := inventory.Baseline.Manifest.Manifests[len(inventory.Baseline.Manifest.Manifests)-1]
	privileges, err := migrate.ParseCanonicalPrivilegeSetBodyV1(latest.CanonicalPrivilegeSet)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	for _, binding := range privileges.RoleBindings {
		if binding.Subject == migrate.AppACLSubjectCenterRuntime {
			return binding.CatalogRole, nil
		}
	}
	return "", ErrRecordLedgerFence
}

func (f *RecordLedgerFence) newPredecessorRoles(phase uint64, realm string) (ledgerPredecessorRoles, string, error) {
	var empty ledgerPredecessorRoles
	runtimeRole, err := ledgerPredecessorRuntime(f.appInventory)
	if err != nil {
		return empty, "", err
	}
	normal := f.appInventory.RoleBaseline
	normal.Roles = append([]ledgerAppRole(nil), normal.Roles...)
	var runtimeSecret string
	secrets := map[string]bool{}
	for index := range normal.Roles {
		role := &normal.Roles[index]
		if role.Name == f.appInventory.Bootstrap {
			continue
		}
		var random [32]byte
		var salt [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return empty, "", ErrRecordLedgerFence
		}
		if _, err := rand.Read(salt[:]); err != nil {
			return empty, "", ErrRecordLedgerFence
		}
		secret := base64.RawURLEncoding.EncodeToString(random[:])
		if secrets[secret] || ledgerMatchesOriginalVerifier(*role, secret) {
			return empty, "", ErrRecordLedgerFence
		}
		for _, target := range f.appInventory.Targets {
			if secret == target.Secret {
				return empty, "", ErrRecordLedgerFence
			}
		}
		secrets[secret] = true
		verifier := ledgerSCRAM(secret, salt[:], 4096)
		role.Verifier = &verifier
		if role.Name == runtimeRole {
			runtimeSecret = secret
		}
	}
	if runtimeSecret == "" {
		return empty, "", ErrRecordLedgerFence
	}
	roles := ledgerPredecessorRoles{Version: 1, PhaseGeneration: phase, RecoveryID: f.request.Compose.RecoveryID, Application: f.request.AppIdentity, RealmDigest: realm, Normal: normal}
	if _, err := ledgerPublishExact(f.control, ledgerPredecessorName("roles", phase), roles); err != nil {
		return empty, "", err
	}
	endpoint := url.URL{Scheme: "postgres", User: url.UserPassword(runtimeRole, runtimeSecret), Host: f.compose.DatabaseAddress + ":5432", Path: "/" + f.request.AppIdentity.DatabaseName, RawQuery: "sslmode=disable"}
	return roles, endpoint.String(), nil
}

func ledgerPredecessorValidateRoles(roles ledgerPredecessorRoles, inventory ledgerAppInventory, intent ledgerPredecessorIntent) error {
	if roles.Version != 1 || roles.PhaseGeneration != intent.PhaseGeneration || roles.RecoveryID != intent.RecoveryID || roles.Application != intent.Application || roles.RealmDigest != intent.RealmDigest {
		return ErrRecordLedgerFence
	}
	expected := inventory.RoleBaseline
	expected.Roles = append([]ledgerAppRole(nil), expected.Roles...)
	if len(expected.Roles) != len(roles.Normal.Roles) {
		return ErrRecordLedgerFence
	}
	seen := map[string]bool{}
	for i := range expected.Roles {
		actual := roles.Normal.Roles[i]
		if actual.Name != expected.Roles[i].Name {
			return ErrRecordLedgerFence
		}
		if actual.Name != inventory.Bootstrap {
			if actual.Verifier == nil || len(*actual.Verifier) < 32 || seen[*actual.Verifier] || reflect.DeepEqual(actual.Verifier, expected.Roles[i].Verifier) {
				return ErrRecordLedgerFence
			}
			for _, target := range inventory.Targets {
				if *actual.Verifier == target.Verifier {
					return ErrRecordLedgerFence
				}
			}
			seen[*actual.Verifier] = true
			expected.Roles[i].Verifier = actual.Verifier
		}
	}
	if !reflect.DeepEqual(expected, roles.Normal) {
		return ErrRecordLedgerFence
	}
	return nil
}

// The entire normal/fenced role change is one transaction. No intermediate
// catalog is admitted, and no fresh secret other than runtime leaves this process.
func ledgerPredecessorApplyRoles(ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory, from, to ledgerAppRoleSnapshot) error {
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	actual, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, from) {
		return ErrRecordLedgerFence
	}
	if err := ledgerDrainAppSessions(ctx, tx, inventory); err != nil {
		return err
	}
	for _, role := range to.Roles {
		if role.Name == inventory.Bootstrap {
			continue
		}
		if role.Verifier == nil {
			return ErrRecordLedgerFence
		}
		var ddl string
		if tx.QueryRow(ctx, `select pg_catalog.format('alter role %I password %L',$1::text,$2::text)`, role.Name, *role.Verifier).Scan(&ddl) != nil {
			return ErrRecordLedgerFence
		}
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return ErrRecordLedgerFence
		}
		login := " nologin"
		if role.Login {
			login = " login"
		}
		if _, err := tx.Exec(ctx, "alter role "+pgx.Identifier{role.Name}.Sanitize()+login); err != nil {
			return ErrRecordLedgerFence
		}
		old, ok := ledgerAppRoleByName(from, role.Name)
		if !ok {
			return ErrRecordLedgerFence
		}
		if old.Connect == role.Connect {
			continue
		}
		if !role.Connect {
			if _, err := tx.Exec(ctx, "revoke connect on database "+pgx.Identifier{inventory.Application.DatabaseName}.Sanitize()+" from "+pgx.Identifier{role.Name}.Sanitize()); err != nil {
				return ErrRecordLedgerFence
			}
		} else {
			found := false
			for _, grant := range to.DatabaseACL {
				if grant.GranteeOID != role.OID || grant.Privilege != "CONNECT" {
					continue
				}
				if found || grant.GrantorOID != to.DatabaseOwnerOID {
					return ErrRecordLedgerFence
				}
				found = true
				suffix := ""
				if grant.Grantable {
					suffix = " with grant option"
				}
				if _, err := tx.Exec(ctx, "grant connect on database "+pgx.Identifier{inventory.Application.DatabaseName}.Sanitize()+" to "+pgx.Identifier{role.Name}.Sanitize()+suffix); err != nil {
					return ErrRecordLedgerFence
				}
			}
			if !found {
				return ErrRecordLedgerFence
			}
		}
	}
	actual, err = ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, to) {
		return ErrRecordLedgerFence
	}
	if err := ledgerDrainAppSessions(ctx, tx, inventory); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func (f *RecordLedgerFence) verifyPredecessorRoles(ctx context.Context, roles ledgerPredecessorRoles) error {
	actual, err := ledgerReadAppRoleSnapshot(ctx, f.app, ledgerAppRoleNames(f.appInventory))
	if err != nil || !reflect.DeepEqual(actual, roles.Normal) {
		return ErrRecordLedgerFence
	}
	snapshot, err := migrate.ReadAppACLMaintenanceSnapshot(ctx, f.app, nil)
	if err != nil || !snapshot.LegacyC80 || !reflect.DeepEqual(snapshot, f.appInventory.Baseline) {
		return ErrRecordLedgerFence
	}
	return ledgerCheckAppSessions(ctx, f.app, f.appInventory, f.appSessions)
}
