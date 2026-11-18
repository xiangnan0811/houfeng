//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/store/migrate"
)

// These values are kept in the protected, immutable per-RID journal. In
// particular, the original verifier and the fresh secrets are needed by the
// separately supervised predecessor and migration phases; they are not an
// authorization to enter either phase.
type ledgerAppInventory struct {
	Version          int                               `json:"version"`
	RecoveryID       string                            `json:"recovery_id"`
	Application      RecordLedgerApplicationIdentity   `json:"application"`
	Bootstrap        string                            `json:"bootstrap"`
	LegacyC80        bool                              `json:"legacy_c80"`
	Admin            string                            `json:"admin"`
	Previous         []string                          `json:"previous"`
	OriginalPrevious []string                          `json:"original_previous,omitempty"`
	ManifestDigest   [32]byte                          `json:"manifest_digest"`
	Baseline         migrate.AppACLMaintenanceSnapshot `json:"baseline"`
	RoleBaseline     ledgerAppRoleSnapshot             `json:"role_baseline"`
	Targets          []ledgerAppRoleTarget             `json:"targets"`
	Credential       ledgerAdminCredential             `json:"credential"`
}

type ledgerAdminCredential struct {
	Version          int    `json:"version"`
	RecoveryID       string `json:"recovery_id"`
	Role             string `json:"role"`
	NewURL           string `json:"new_url"`
	PreviousVerifier string `json:"previous_verifier"`
	NewVerifier      string `json:"new_verifier"`
}

type ledgerAppRole struct {
	Name        string  `json:"name"`
	OID         uint32  `json:"oid"`
	Superuser   bool    `json:"superuser"`
	Inherit     bool    `json:"inherit"`
	CreateRole  bool    `json:"create_role"`
	CreateDB    bool    `json:"create_db"`
	Login       bool    `json:"login"`
	Replication bool    `json:"replication"`
	BypassRLS   bool    `json:"bypass_rls"`
	ConnLimit   int32   `json:"connection_limit"`
	Verifier    *string `json:"verifier"`
	ValidUntil  *string `json:"valid_until"`
	Connect     bool    `json:"connect"`
}

type ledgerAppRoleMembership struct {
	RoleOID       uint32 `json:"role_oid"`
	MemberOID     uint32 `json:"member_oid"`
	GrantorOID    uint32 `json:"grantor_oid"`
	AdminOption   bool   `json:"admin_option"`
	InheritOption bool   `json:"inherit_option"`
	SetOption     bool   `json:"set_option"`
}

type ledgerAppRoleSetting struct {
	RoleOID     uint32   `json:"role_oid"`
	DatabaseOID uint32   `json:"database_oid"`
	Database    string   `json:"database"`
	Config      []string `json:"config"`
}

type ledgerAppDatabaseACL struct {
	GrantorOID uint32 `json:"grantor_oid"`
	GranteeOID uint32 `json:"grantee_oid"`
	Privilege  string `json:"privilege"`
	Grantable  bool   `json:"grantable"`
}

type ledgerAppRoleSnapshot struct {
	DatabaseOID      uint32                    `json:"database_oid"`
	DatabaseOwnerOID uint32                    `json:"database_owner_oid"`
	Roles            []ledgerAppRole           `json:"roles"`
	Memberships      []ledgerAppRoleMembership `json:"memberships"`
	Settings         []ledgerAppRoleSetting    `json:"settings"`
	DatabaseACL      []ledgerAppDatabaseACL    `json:"database_acl"`
}

type ledgerAppRoleTarget struct {
	Role     string `json:"role"`
	Secret   string `json:"secret"`
	Verifier string `json:"verifier"`
}

type ledgerAppState uint8

const (
	ledgerAppOriginal ledgerAppState = iota
	ledgerAppTarget
	ledgerAppTargetAdminClosed
)

var errLedgerAppMigrationTransition = errors.New("record ledger APP C80 to C81 transition requires the registered migration hook")

type ledgerAppQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ledgerAppPrincipalInventory derives the exact role list from a verified C80
// or current C81 snapshot. C80 has no source_application_principals table.
func ledgerAppPrincipalInventory(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, bootstrap string) (ledgerAppInventory, error) {
	snapshot, err := migrate.ReadAppACLMaintenanceSnapshot(ctx, app, nil)
	if err != nil {
		return ledgerAppInventory{}, ErrRecordLedgerFence
	}
	inventory := ledgerAppInventory{
		Version: 2, RecoveryID: request.Compose.RecoveryID, Application: request.AppIdentity,
		Bootstrap: bootstrap, LegacyC80: snapshot.LegacyC80, Admin: request.AppAdminRole,
		Previous: append([]string{}, request.AppPreviousLogins...), Baseline: snapshot,
	}
	sort.Strings(inventory.Previous)
	if err := ledgerAppPrincipalMatches(ctx, app, inventory, snapshot); err != nil {
		return ledgerAppInventory{}, err
	}
	inventory.ManifestDigest = snapshot.Manifest.Manifests[len(snapshot.Manifest.Manifests)-1].ManifestDigest
	return inventory, nil
}

func ledgerAppPrincipalMatches(ctx context.Context, app ledgerAppQueryer, inventory ledgerAppInventory, snapshot migrate.AppACLMaintenanceSnapshot) error {
	manifest := snapshot.Manifest
	if !ledgerSafeRole(inventory.Bootstrap) || !ledgerSafeRole(inventory.Admin) || inventory.Admin == inventory.Bootstrap ||
		manifest.Head == nil || len(manifest.Manifests) == 0 || manifest.DatabaseName != inventory.Application.DatabaseName ||
		manifest.SessionUser != inventory.Bootstrap || manifest.CurrentUser != inventory.Bootstrap {
		return ErrRecordLedgerFence
	}
	latest := manifest.Manifests[len(manifest.Manifests)-1]
	privileges, err := migrate.ParseCanonicalPrivilegeSetBodyV2(latest.CanonicalPrivilegeSet)
	if err != nil {
		privileges, err = migrate.ParseCanonicalPrivilegeSetBodyV1(latest.CanonicalPrivilegeSet)
	}
	if err != nil {
		return ErrRecordLedgerFence
	}
	expected := make(map[string]bool)
	adminFound := false
	for _, binding := range privileges.RoleBindings {
		if binding.Subject == migrate.AppACLSubjectPlatformAdmin {
			if adminFound || binding.CatalogRole != inventory.Admin {
				return ErrRecordLedgerFence
			}
			adminFound = true
		} else if binding.CatalogRole != inventory.Bootstrap {
			expected[binding.CatalogRole] = true
		}
	}
	if !adminFound {
		return ErrRecordLedgerFence
	}
	if latest.MigratorCatalogRole != inventory.Bootstrap {
		expected[latest.MigratorCatalogRole] = true
	}
	// This also includes every present, manifest-checked auxiliary role.
	for _, role := range snapshot.Catalog.Roles {
		if role.Name != inventory.Admin && role.Name != inventory.Bootstrap {
			expected[role.Name] = true
		}
	}
	var authorityExists bool
	if app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeAuthorityRole).Scan(&authorityExists) != nil {
		return ErrRecordLedgerFence
	}
	if authorityExists {
		if composeAuthorityRole == inventory.Bootstrap || composeAuthorityRole == inventory.Admin {
			return ErrRecordLedgerFence
		}
		expected[composeAuthorityRole] = true
	}
	if !snapshot.LegacyC80 {
		rows, err := app.Query(ctx, `select distinct principal::text from public.source_application_principals`)
		if err != nil {
			return ErrRecordLedgerFence
		}
		for rows.Next() {
			var role string
			if rows.Scan(&role) != nil || !ledgerSafeRole(role) {
				rows.Close()
				return ErrRecordLedgerFence
			}
			if role != inventory.Admin && role != inventory.Bootstrap {
				expected[role] = true
			}
		}
		if rows.Err() != nil {
			rows.Close()
			return ErrRecordLedgerFence
		}
		rows.Close()
	}
	if len(inventory.Previous) != len(expected) {
		return ErrRecordLedgerFence
	}
	for i, role := range inventory.Previous {
		if !ledgerSafeRole(role) || !expected[role] || role == inventory.Admin || role == inventory.Bootstrap ||
			(i > 0 && inventory.Previous[i-1] >= role) {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func ledgerReadAppRoles(ctx context.Context, q ledgerAppQueryer, names []string) (ledgerAppRoleSnapshot, error) {
	var snapshot ledgerAppRoleSnapshot
	if q.QueryRow(ctx, `select oid,datdba from pg_catalog.pg_database where datname=pg_catalog.current_database()`).Scan(&snapshot.DatabaseOID, &snapshot.DatabaseOwnerOID) != nil {
		return snapshot, ErrRecordLedgerFence
	}
	rows, err := q.Query(ctx, `select r.rolname::text,r.oid,r.rolsuper,r.rolinherit,r.rolcreaterole,r.rolcreatedb,
		r.rolcanlogin,r.rolreplication,r.rolbypassrls,r.rolconnlimit,r.rolpassword,extract(epoch from r.rolvaliduntil)::text,
		pg_catalog.has_database_privilege(r.oid,pg_catalog.current_database(),'CONNECT')
		from pg_catalog.pg_authid r where r.rolname=any($1::name[]) order by r.rolname`, names)
	if err != nil {
		return snapshot, ErrRecordLedgerFence
	}
	snapshot.Roles = make([]ledgerAppRole, 0, len(names))
	var oids []uint32
	for rows.Next() {
		var role ledgerAppRole
		if rows.Scan(&role.Name, &role.OID, &role.Superuser, &role.Inherit, &role.CreateRole, &role.CreateDB,
			&role.Login, &role.Replication, &role.BypassRLS, &role.ConnLimit, &role.Verifier, &role.ValidUntil, &role.Connect) != nil {
			rows.Close()
			return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		snapshot.Roles = append(snapshot.Roles, role)
		oids = append(oids, role.OID)
	}
	if rows.Err() != nil || len(snapshot.Roles) != len(names) {
		rows.Close()
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	rows.Close()
	rows, err = q.Query(ctx, `select roleid,member,grantor,admin_option,inherit_option,set_option
		from pg_catalog.pg_auth_members where roleid=any($1::oid[]) or member=any($1::oid[]) or grantor=any($1::oid[])
		order by roleid,member,grantor`, oids)
	if err != nil {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	snapshot.Memberships = make([]ledgerAppRoleMembership, 0)
	for rows.Next() {
		var member ledgerAppRoleMembership
		if rows.Scan(&member.RoleOID, &member.MemberOID, &member.GrantorOID, &member.AdminOption, &member.InheritOption, &member.SetOption) != nil {
			rows.Close()
			return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		snapshot.Memberships = append(snapshot.Memberships, member)
	}
	if rows.Err() != nil {
		rows.Close()
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	rows.Close()
	rows, err = q.Query(ctx, `select s.setrole,s.setdatabase,coalesce(d.datname::text,''),s.setconfig
		from pg_catalog.pg_db_role_setting s left join pg_catalog.pg_database d on d.oid=s.setdatabase
		where s.setrole=any($1::oid[]) order by s.setrole,s.setdatabase`, oids)
	if err != nil {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	snapshot.Settings = make([]ledgerAppRoleSetting, 0)
	for rows.Next() {
		var setting ledgerAppRoleSetting
		if rows.Scan(&setting.RoleOID, &setting.DatabaseOID, &setting.Database, &setting.Config) != nil {
			rows.Close()
			return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		snapshot.Settings = append(snapshot.Settings, setting)
	}
	if rows.Err() != nil {
		rows.Close()
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	rows.Close()
	rows, err = q.Query(ctx, `select acl.grantor,acl.grantee,acl.privilege_type,acl.is_grantable
		from pg_catalog.pg_database d cross join lateral
		pg_catalog.aclexplode(coalesce(d.datacl,pg_catalog.acldefault('d',d.datdba))) acl
		where d.oid=$1 order by acl.grantor,acl.grantee,acl.privilege_type,acl.is_grantable`, snapshot.DatabaseOID)
	if err != nil {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	snapshot.DatabaseACL = make([]ledgerAppDatabaseACL, 0)
	for rows.Next() {
		var grant ledgerAppDatabaseACL
		if rows.Scan(&grant.GrantorOID, &grant.GranteeOID, &grant.Privilege, &grant.Grantable) != nil {
			rows.Close()
			return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		snapshot.DatabaseACL = append(snapshot.DatabaseACL, grant)
	}
	if rows.Err() != nil {
		rows.Close()
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	rows.Close()
	return snapshot, nil
}

// Read all role, membership, setting and ACL facts in one snapshot.
func ledgerReadAppRoleSnapshot(ctx context.Context, app *pgxpool.Pool, names []string) (ledgerAppRoleSnapshot, error) {
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	snapshot, err := ledgerReadAppRoles(ctx, tx, names)
	if err != nil {
		return ledgerAppRoleSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	return snapshot, nil
}

func ledgerAppRoleNames(inventory ledgerAppInventory) []string {
	names := append([]string{}, inventory.Previous...)
	names = append(names, inventory.Admin, inventory.Bootstrap)
	sort.Strings(names)
	return names
}

func ledgerAppRoleByName(snapshot ledgerAppRoleSnapshot, name string) (ledgerAppRole, bool) {
	for _, role := range snapshot.Roles {
		if role.Name == name {
			return role, true
		}
	}
	return ledgerAppRole{}, false
}

func ledgerValidateOriginalRoles(inventory ledgerAppInventory) error {
	baseline := inventory.RoleBaseline
	if baseline.DatabaseOID != inventory.Application.DatabaseOID || baseline.DatabaseOwnerOID == 0 || len(baseline.Roles) != len(ledgerAppRoleNames(inventory)) {
		return ErrRecordLedgerFence
	}
	names := ledgerAppRoleNames(inventory)
	for i, role := range baseline.Roles {
		if role.Name != names[i] || role.OID == 0 || (i > 0 && names[i-1] >= names[i]) {
			return ErrRecordLedgerFence
		}
	}
	admin, present := ledgerAppRoleByName(baseline, inventory.Admin)
	if !present || !admin.Login || !admin.Connect || admin.Verifier == nil {
		return ErrRecordLedgerFence
	}
	protected := make(map[uint32]bool)
	previous := make(map[uint32]bool)
	for _, name := range append(append([]string{}, inventory.Previous...), inventory.Admin) {
		role, ok := ledgerAppRoleByName(baseline, name)
		if !ok || role.OID == 0 || role.Superuser || role.CreateRole || role.CreateDB || role.Replication || role.BypassRLS {
			return ErrRecordLedgerFence
		}
		protected[role.OID] = true
		if name != inventory.Admin {
			previous[role.OID] = true
		}
	}
	for _, member := range baseline.Memberships {
		if protected[member.RoleOID] || protected[member.MemberOID] {
			return ErrRecordLedgerFence
		}
	}
	adminConnect := false
	for _, grant := range baseline.DatabaseACL {
		if grant.Privilege != "CONNECT" {
			continue
		}
		// PUBLIC or a different grantor could leave a retired role connected
		// after the single, restricted database REVOKE.
		if grant.GranteeOID == 0 || (previous[grant.GranteeOID] && grant.GrantorOID != baseline.DatabaseOwnerOID) {
			return ErrRecordLedgerFence
		}
		if grant.GranteeOID == admin.OID {
			adminConnect = true
		}
	}
	if !adminConnect {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerAppExpectedRoles(inventory ledgerAppInventory, state ledgerAppState) ledgerAppRoleSnapshot {
	expected := inventory.RoleBaseline
	if state == ledgerAppOriginal {
		return expected
	}
	expected.Roles = append([]ledgerAppRole{}, expected.Roles...)
	previousOIDs := make(map[uint32]bool, len(inventory.Previous))
	for i := range expected.Roles {
		for _, target := range inventory.Targets {
			if expected.Roles[i].Name != target.Role {
				continue
			}
			verifier := target.Verifier
			expected.Roles[i].Verifier = &verifier
			if target.Role == inventory.Admin {
				expected.Roles[i].Login = state == ledgerAppTarget
			} else {
				expected.Roles[i].Login = false
				expected.Roles[i].Connect = false
				previousOIDs[expected.Roles[i].OID] = true
			}
		}
	}
	expected.DatabaseACL = make([]ledgerAppDatabaseACL, 0, len(inventory.RoleBaseline.DatabaseACL))
	for _, grant := range inventory.RoleBaseline.DatabaseACL {
		if grant.Privilege != "CONNECT" || !previousOIDs[grant.GranteeOID] {
			expected.DatabaseACL = append(expected.DatabaseACL, grant)
		}
	}
	return expected
}

func ledgerAppExpectedCatalog(inventory ledgerAppInventory, state ledgerAppState) migrate.AppACLEffectiveCatalogSnapshotR1 {
	catalog := inventory.Baseline.Catalog
	if state == ledgerAppOriginal {
		return catalog
	}
	previous := make(map[string]bool, len(inventory.Previous))
	for _, name := range inventory.Previous {
		previous[name] = true
	}
	catalog.Roles = append([]migrate.AppACLEffectiveCatalogRoleStateR1{}, catalog.Roles...)
	for i := range catalog.Roles {
		if previous[catalog.Roles[i].Name] {
			catalog.Roles[i].Login = false
		} else if catalog.Roles[i].Name == inventory.Admin && state == ledgerAppTargetAdminClosed {
			catalog.Roles[i].Login = false
		}
	}
	filter := func(input []migrate.AppACLEffectiveCatalogPrivilegeObservationR1) []migrate.AppACLEffectiveCatalogPrivilegeObservationR1 {
		output := make([]migrate.AppACLEffectiveCatalogPrivilegeObservationR1, 0, len(input))
		for _, p := range input {
			if previous[p.Grantee] && p.ObjectClass == migrate.AppACLObjectClassDatabase && p.Privilege == migrate.AppACLPrivilegeConnect {
				continue
			}
			output = append(output, p)
		}
		return output
	}
	catalog.DirectPrivileges = filter(catalog.DirectPrivileges)
	catalog.EffectivePrivileges = filter(catalog.EffectivePrivileges)
	return catalog
}

func ledgerAppTrustedStates(inventory ledgerAppInventory, state ledgerAppState) []migrate.AppACLMaintenanceRoleState {
	if state == ledgerAppOriginal {
		return nil
	}
	expected := ledgerAppExpectedRoles(inventory, state)
	var states []migrate.AppACLMaintenanceRoleState
	for _, catalogRole := range inventory.Baseline.Catalog.Roles {
		if catalogRole.Name == inventory.Bootstrap {
			continue
		}
		if role, found := ledgerAppRoleByName(expected, catalogRole.Name); found {
			states = append(states, migrate.AppACLMaintenanceRoleState{Role: role.Name, Login: role.Login, Connect: role.Connect})
		}
	}
	return states
}

func ledgerValidateAppJournal(inventory ledgerAppInventory, credential ledgerAdminCredential) error {
	if inventory.Version != 2 || inventory.RecoveryID == "" || inventory.Baseline.LegacyC80 != inventory.LegacyC80 ||
		inventory.Admin != credential.Role || inventory.RecoveryID != credential.RecoveryID || credential.Version != 1 ||
		!reflect.DeepEqual(inventory.Credential, credential) || len(inventory.Baseline.Manifest.Manifests) == 0 ||
		inventory.Baseline.Manifest.Manifests[len(inventory.Baseline.Manifest.Manifests)-1].ManifestDigest != inventory.ManifestDigest ||
		ledgerValidateOriginalRoles(inventory) != nil {
		return ErrRecordLedgerFence
	}
	admin, _ := ledgerAppRoleByName(inventory.RoleBaseline, inventory.Admin)
	if admin.Verifier == nil || credential.NewURL == "" {
		return ErrRecordLedgerFence
	}
	if inventory.OriginalPrevious == nil {
		if *admin.Verifier != credential.PreviousVerifier {
			return ErrRecordLedgerFence
		}
	} else {
		original := append([]string{}, inventory.OriginalPrevious...)
		sort.Strings(original)
		if inventory.LegacyC80 || !reflect.DeepEqual(original, inventory.OriginalPrevious) {
			return ErrRecordLedgerFence
		}
		original = append(original, composeDeletionWorkerRole)
		sort.Strings(original)
		if !reflect.DeepEqual(original, inventory.Previous) {
			return ErrRecordLedgerFence
		}
		for i := 1; i < len(original); i++ {
			if original[i-1] == original[i] {
				return ErrRecordLedgerFence
			}
		}
	}
	if len(inventory.Targets) != len(inventory.Previous)+1 {
		return ErrRecordLedgerFence
	}
	secrets := make(map[string]bool, len(inventory.Targets))
	for i, target := range inventory.Targets {
		if !ledgerSafeRole(target.Role) || len(target.Secret) < 32 || secrets[target.Secret] ||
			!ledgerMatchesSCRAM(target.Verifier, target.Secret) || (i > 0 && inventory.Targets[i-1].Role >= target.Role) {
			return ErrRecordLedgerFence
		}
		secrets[target.Secret] = true
		original, found := ledgerAppRoleByName(inventory.RoleBaseline, target.Role)
		if !found || target.Role == inventory.Bootstrap || (original.Verifier != nil && ledgerMatchesSCRAM(*original.Verifier, target.Secret)) {
			return ErrRecordLedgerFence
		}
		if target.Role == inventory.Admin && target.Verifier != credential.NewVerifier {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

// This verifies the actual catalog and both independent role reads against the
// exact transformation of the protected original, including every old verifier.
func ledgerVerifyAppRoleState(ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory, credential ledgerAdminCredential, state ledgerAppState) error {
	if ledgerValidateAppJournal(inventory, credential) != nil {
		return ErrRecordLedgerFence
	}
	names := ledgerAppRoleNames(inventory)
	before, err := ledgerReadAppRoleSnapshot(ctx, app, names)
	if err != nil {
		return ErrRecordLedgerFence
	}
	actual, err := migrate.ReadAppACLMaintenanceSnapshot(ctx, app, ledgerAppTrustedStates(inventory, state))
	if err != nil {
		return ErrRecordLedgerFence
	}
	if inventory.LegacyC80 && !actual.LegacyC80 {
		return errLedgerAppMigrationTransition
	}
	if actual.LegacyC80 != inventory.LegacyC80 || !reflect.DeepEqual(actual.Manifest, inventory.Baseline.Manifest) ||
		!reflect.DeepEqual(actual.Catalog, ledgerAppExpectedCatalog(inventory, state)) ||
		ledgerAppPrincipalMatches(ctx, app, inventory, actual) != nil {
		return ErrRecordLedgerFence
	}
	after, err := ledgerReadAppRoleSnapshot(ctx, app, names)
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(after, ledgerAppExpectedRoles(inventory, state)) {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerMakeAppTargets(inventory *ledgerAppInventory, request RecordLedgerMaintenanceRequest, address string) error {
	old, e1 := ledgerParsePool(request.AppPreviousAdminURL)
	fresh, e2 := ledgerParsePool(request.AppAdminURL)
	bootstrap, e3 := ledgerParsePool(request.AppBootstrapURL)
	if e1 != nil || e2 != nil || e3 != nil || old.ConnConfig.User != inventory.Admin || fresh.ConnConfig.User != inventory.Admin ||
		bootstrap.ConnConfig.User != inventory.Bootstrap || old.ConnConfig.Host != fresh.ConnConfig.Host || old.ConnConfig.Host != bootstrap.ConnConfig.Host ||
		old.ConnConfig.Port != fresh.ConnConfig.Port || old.ConnConfig.Port != bootstrap.ConnConfig.Port ||
		old.ConnConfig.Database != inventory.Application.DatabaseName || fresh.ConnConfig.Database != inventory.Application.DatabaseName ||
		bootstrap.ConnConfig.Database != inventory.Application.DatabaseName || old.ConnConfig.Password == fresh.ConnConfig.Password ||
		len(fresh.ConnConfig.Password) < 32 || ledgerPinApp(fresh, address) != nil {
		return ErrRecordLedgerFence
	}
	admin, ok := ledgerAppRoleByName(inventory.RoleBaseline, inventory.Admin)
	if !ok || admin.Verifier == nil || !ledgerMatchesSCRAM(*admin.Verifier, old.ConnConfig.Password) || ledgerMatchesSCRAM(*admin.Verifier, fresh.ConnConfig.Password) {
		return ErrRecordLedgerFence
	}
	names := append(append([]string{}, inventory.Previous...), inventory.Admin)
	sort.Strings(names)
	inventory.Targets = make([]ledgerAppRoleTarget, 0, len(names))
	used := map[string]bool{fresh.ConnConfig.Password: true, old.ConnConfig.Password: true}
	for _, name := range names {
		secret := fresh.ConnConfig.Password
		if name != inventory.Admin {
			var random [32]byte
			if _, err := rand.Read(random[:]); err != nil {
				return ErrRecordLedgerFence
			}
			secret = base64.RawURLEncoding.EncodeToString(random[:])
			if used[secret] {
				return ErrRecordLedgerFence
			}
			used[secret] = true
		}
		var salt [16]byte
		if _, err := rand.Read(salt[:]); err != nil {
			return ErrRecordLedgerFence
		}
		verifier := ledgerSCRAM(secret, salt[:], 4096)
		inventory.Targets = append(inventory.Targets, ledgerAppRoleTarget{Role: name, Secret: secret, Verifier: verifier})
		if name == inventory.Admin {
			inventory.Credential = ledgerAdminCredential{Version: 1, RecoveryID: inventory.RecoveryID, Role: name,
				NewURL: request.AppAdminURL, PreviousVerifier: *admin.Verifier, NewVerifier: verifier}
		}
	}
	return nil
}

// PostgreSQL can retain a legacy MD5 verifier even though all new credentials
// use SCRAM. Compare it only to rule out reusing an old role's secret.
func ledgerMatchesOriginalVerifier(role ledgerAppRole, secret string) bool {
	if role.Verifier == nil {
		return false
	}
	if strings.HasPrefix(*role.Verifier, "md5") {
		digest := md5.Sum([]byte(secret + role.Name))
		candidate := "md5" + hex.EncodeToString(digest[:])
		return subtle.ConstantTimeCompare([]byte(candidate), []byte(*role.Verifier)) == 1
	}
	return ledgerMatchesSCRAM(*role.Verifier, secret)
}

func ledgerValidateAppRequest(inventory ledgerAppInventory, request RecordLedgerMaintenanceRequest, bootstrap, address string) error {
	previous := append([]string{}, request.AppPreviousLogins...)
	sort.Strings(previous)
	requestPrevious := inventory.Previous
	if inventory.OriginalPrevious != nil {
		requestPrevious = inventory.OriginalPrevious
	}
	if inventory.RecoveryID != request.Compose.RecoveryID || inventory.Application != request.AppIdentity || inventory.Bootstrap != bootstrap ||
		inventory.Admin != request.AppAdminRole || !reflect.DeepEqual(requestPrevious, previous) || inventory.Credential.NewURL != request.AppAdminURL {
		return ErrRecordLedgerFence
	}
	old, e1 := ledgerParsePool(request.AppPreviousAdminURL)
	fresh, e2 := ledgerParsePool(request.AppAdminURL)
	boot, e3 := ledgerParsePool(request.AppBootstrapURL)
	if e1 != nil || e2 != nil || e3 != nil || old.ConnConfig.User != inventory.Admin || fresh.ConnConfig.User != inventory.Admin || boot.ConnConfig.User != bootstrap ||
		old.ConnConfig.Host != fresh.ConnConfig.Host || old.ConnConfig.Host != boot.ConnConfig.Host || old.ConnConfig.Port != fresh.ConnConfig.Port || old.ConnConfig.Port != boot.ConnConfig.Port ||
		old.ConnConfig.Database != inventory.Application.DatabaseName || fresh.ConnConfig.Database != inventory.Application.DatabaseName || boot.ConnConfig.Database != inventory.Application.DatabaseName ||
		old.ConnConfig.Password == fresh.ConnConfig.Password || len(fresh.ConnConfig.Password) < 32 || ledgerPinApp(fresh, address) != nil ||
		!ledgerMatchesSCRAM(inventory.Credential.PreviousVerifier, old.ConnConfig.Password) || !ledgerMatchesSCRAM(inventory.Credential.NewVerifier, fresh.ConnConfig.Password) {
		return ErrRecordLedgerFence
	}
	for _, target := range inventory.Targets {
		if target.Role == inventory.Admin && target.Secret != fresh.ConnConfig.Password {
			return ErrRecordLedgerFence
		}
	}
	if err := ledgerValidateAppJournal(inventory, inventory.Credential); err != nil {
		return err
	}
	for _, target := range inventory.Targets {
		for _, original := range inventory.RoleBaseline.Roles {
			if ledgerMatchesOriginalVerifier(original, target.Secret) {
				return ErrRecordLedgerFence
			}
		}
	}
	return nil
}

func ledgerDrainAppSessions(ctx context.Context, tx pgx.Tx, inventory ledgerAppInventory) error {
	roles := append(append([]string{}, inventory.Previous...), inventory.Admin)
	var prepared int
	if tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, roles).Scan(&prepared) != nil || prepared != 0 {
		return ErrRecordLedgerFence
	}
	rows, err := tx.Query(ctx, `select pg_catalog.pg_terminate_backend(pid) from pg_catalog.pg_stat_activity
		where backend_type='client backend' and (datname=pg_catalog.current_database() or usename=any($1::name[])) and pid<>pg_catalog.pg_backend_pid()`, roles)
	if err != nil {
		return ErrRecordLedgerFence
	}
	for rows.Next() {
		var stopped bool
		if rows.Scan(&stopped) != nil || !stopped {
			rows.Close()
			return ErrRecordLedgerFence
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrRecordLedgerFence
	}
	rows.Close()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var active int
		if _, err := tx.Exec(ctx, `select pg_catalog.pg_stat_clear_snapshot()`); err != nil {
			return ErrRecordLedgerFence
		}
		if tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where backend_type='client backend'
			and (datname=pg_catalog.current_database() or usename=any($1::name[])) and pid<>pg_catalog.pg_backend_pid()`, roles).Scan(&active) != nil ||
			tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, roles).Scan(&prepared) != nil || prepared != 0 {
			return ErrRecordLedgerFence
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrRecordLedgerFence
		case <-ticker.C:
		}
	}
}

func ledgerCheckAppPreparationSessions(ctx context.Context, app *pgxpool.Pool, bootstrap ledgerAppSession, inventory ledgerAppInventory) error {
	current, err := ledgerCurrentAppSession(ctx, app)
	if err != nil || current.PID != bootstrap.PID || current.Role != bootstrap.Role || !current.Started.Equal(bootstrap.Started) {
		return ErrRecordLedgerFence
	}
	roles := append(append([]string{}, inventory.Previous...), inventory.Admin)
	var active, prepared int
	if app.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where backend_type='client backend'
		and (datname=pg_catalog.current_database() or usename=any($1::name[])) and pid<>pg_catalog.pg_backend_pid()`, roles).Scan(&active) != nil {
		return ErrRecordLedgerFence
	}
	if active != 0 {
		return ErrRecordLedgerUnexpectedSession
	}
	if app.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, roles).Scan(&prepared) != nil || prepared != 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerApplyAppRoles(ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory, from ledgerAppState, bootstrap ledgerAppSession) error {
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	var current ledgerAppSession
	if tx.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&current.PID, &current.Started, &current.Role) != nil ||
		current.PID != bootstrap.PID || current.Role != bootstrap.Role || !current.Started.Equal(bootstrap.Started) {
		return ErrRecordLedgerFence
	}
	actual, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, ledgerAppExpectedRoles(inventory, from)) {
		return ErrRecordLedgerFence
	}
	if err := ledgerDrainAppSessions(ctx, tx, inventory); err != nil {
		return err
	}
	if from == ledgerAppOriginal {
		for _, target := range inventory.Targets {
			var ddl string
			if tx.QueryRow(ctx, `select pg_catalog.format('alter role %I password %L',$1::text,$2::text)`, target.Role, target.Verifier).Scan(&ddl) != nil {
				return ErrRecordLedgerFence
			}
			if _, err := tx.Exec(ctx, ddl); err != nil {
				return ErrRecordLedgerFence
			}
			if target.Role != inventory.Admin {
				if _, err := tx.Exec(ctx, "alter role "+pgx.Identifier{target.Role}.Sanitize()+" nologin"); err != nil {
					return ErrRecordLedgerFence
				}
				if _, err := tx.Exec(ctx, "revoke connect on database "+pgx.Identifier{inventory.Application.DatabaseName}.Sanitize()+" from "+pgx.Identifier{target.Role}.Sanitize()); err != nil {
					return ErrRecordLedgerFence
				}
			}
		}
	}
	if from != ledgerAppTarget {
		if _, err := tx.Exec(ctx, "alter role "+pgx.Identifier{inventory.Admin}.Sanitize()+" login"); err != nil {
			return ErrRecordLedgerFence
		}
	}
	actual, err = ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, ledgerAppExpectedRoles(inventory, ledgerAppTarget)) {
		return ErrRecordLedgerFence
	}
	if err := ledgerDrainAppSessions(ctx, tx, inventory); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// The caller resolves an ambiguous commit only against the protected
		// all-original or all-target snapshots, never an individual role.
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerVerifyAdminCredential(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, address string, credential ledgerAdminCredential) error {
	test, err := ledgerOpenApp(ctx, credential.NewURL, address, request.AppIdentity, false, app, 5432, false)
	if err != nil {
		return ErrRecordLedgerFence
	}
	test.Close()
	old, err := ledgerParsePool(request.AppPreviousAdminURL)
	if err != nil || ledgerPinApp(old, address) != nil {
		return ErrRecordLedgerFence
	}
	conn, err := pgx.ConnectConfig(ctx, old.ConnConfig)
	if err == nil {
		conn.Close(ctx)
		return ErrRecordLedgerFence
	}
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "28P01" {
		return ErrRecordLedgerFence
	}
	return nil
}

// An exact current migration manifest after a C80 journal is a reason to
// stop, not catalog admission or authority to perform the transition.
func ledgerCurrentManifestObserved(ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory) bool {
	snapshot, err := migrate.NewPostgresAppACLManifestRuntimeReader(app).ReadAppACLManifestRuntimeSnapshotV1(ctx)
	if err != nil || snapshot.Head == nil || len(snapshot.Manifests) == 0 ||
		snapshot.DatabaseName != inventory.Application.DatabaseName || snapshot.SessionUser != inventory.Bootstrap || snapshot.CurrentUser != inventory.Bootstrap ||
		migrate.ValidateAppACLManifestChainV1(snapshot.Manifests, *snapshot.Head) != nil {
		return false
	}
	applied, e1 := migrate.CanonicalMigrationSetBodyV1(snapshot.AppliedMigrations)
	current, e2 := migrate.CanonicalMigrationSetFromFS(migrations.FS)
	return e1 == nil && e2 == nil && bytes.Equal(applied, current) &&
		bytes.Equal(snapshot.Manifests[len(snapshot.Manifests)-1].CanonicalMigrationSet, current)
}

func ledgerAppObservedState(ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory, credential ledgerAdminCredential) (ledgerAppState, error) {
	if inventory.LegacyC80 && ledgerCurrentManifestObserved(ctx, app, inventory) {
		return 0, errLedgerAppMigrationTransition
	}
	actual, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(inventory))
	if err != nil {
		return 0, ErrRecordLedgerFence
	}
	for _, state := range []ledgerAppState{ledgerAppOriginal, ledgerAppTarget, ledgerAppTargetAdminClosed} {
		if reflect.DeepEqual(actual, ledgerAppExpectedRoles(inventory, state)) {
			if err := ledgerVerifyAppRoleState(ctx, app, inventory, credential, state); err != nil {
				return 0, err
			}
			return state, nil
		}
	}
	return 0, ErrRecordLedgerFence
}

// ledgerPrepareApplication is the sole entrypoint for the atomic APP role
// cutover. The complete baseline and every target are durably published before
// the first ALTER/REVOKE. No callback or migration authority is conferred.
func ledgerPrepareApplication(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, bootstrap string, control, session int, address string, checkpointer ledgerMaintenanceCheckpointer) (inventory ledgerAppInventory, credential ledgerAdminCredential, result error) {
	if ctx == nil || app == nil || app.Config().MaxConns != 1 || !ledgerControlMatches(request.Compose.ControlConfigPath, control) ||
		!ledgerSessionMatches(control, session, request.Compose.RecoveryID) || ledgerAppIdentity(ctx, app, request.AppIdentity) != nil ||
		ledgerDirectRole(ctx, app, bootstrap, true) != nil {
		return inventory, credential, ErrRecordLedgerFence
	}
	bootstrapSession, err := ledgerCurrentAppSession(ctx, app)
	if err != nil || bootstrapSession.Role != bootstrap {
		return inventory, credential, ErrRecordLedgerFence
	}
	activated, hasActivation, err := ledgerMigrationActivatedInventory(ctx, control, request, checkpointer)
	if err != nil {
		return inventory, credential, ErrRecordLedgerFence
	}
	data, exists, err := fenceReadFile(session, "app-inventory.json")
	if err != nil {
		return inventory, credential, ErrRecordLedgerFence
	}
	if hasActivation {
		if activated.RecoveryID != request.Compose.RecoveryID || ledgerValidateAppRequest(activated, request, bootstrap, address) != nil {
			return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
		}
		inventory = activated
	} else if exists {
		if fenceDecode(data, &inventory) != nil || inventory.OriginalPrevious != nil || ledgerValidateAppRequest(inventory, request, bootstrap, address) != nil {
			return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
		}
	} else {
		inventory, err = ledgerAppPrincipalInventory(ctx, app, request, bootstrap)
		if err != nil || ledgerCheckAppLoginInventory(ctx, app, bootstrap, inventory.Admin, inventory.Previous, false) != nil {
			return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
		}
		inventory.RoleBaseline, err = ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(inventory))
		if err != nil || ledgerValidateOriginalRoles(inventory) != nil || ledgerMakeAppTargets(&inventory, request, address) != nil ||
			ledgerValidateAppRequest(inventory, request, bootstrap, address) != nil {
			return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
		}
		if err := ledgerVerifyAppRoleState(ctx, app, inventory, inventory.Credential, ledgerAppOriginal); err != nil {
			return ledgerAppInventory{}, ledgerAdminCredential{}, err
		}
		if _, err := ledgerPublishExact(session, "app-inventory.json", inventory); err != nil {
			return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
		}
	}
	credential = inventory.Credential
	// Close a journal-bound fresh admin on any later failure, including a
	// contradictory catalog. This never authorizes or repairs a mixed state.
	protectedInventory := inventory
	defer func() {
		if result == nil {
			return
		}
		closeCtx, cancel, err := ledgerMaintenanceCleanupContext(ctx)
		if err != nil {
			result = errors.Join(result, err)
			return
		}
		defer cancel()
		actual, err := ledgerReadAppRoleSnapshot(closeCtx, app, ledgerAppRoleNames(protectedInventory))
		if err != nil {
			result = errors.Join(result, err)
			return
		}
		currentAdmin, found := ledgerAppRoleByName(actual, protectedInventory.Admin)
		expectedAdmin, expected := ledgerAppRoleByName(ledgerAppExpectedRoles(protectedInventory, ledgerAppTarget), protectedInventory.Admin)
		if found && expected && reflect.DeepEqual(currentAdmin, expectedAdmin) {
			result = errors.Join(result, ledgerDisableAdmin(closeCtx, app, protectedInventory.Admin))
		}
	}()
	if _, err := ledgerPublishExact(session, "management-credential.json", credential); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
	}
	state, err := ledgerAppObservedState(ctx, app, inventory, credential)
	if err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	// Exact-target reentry also drains sessions, without changing any roles.
	if err := ledgerApplyAppRoles(ctx, app, inventory, state, bootstrapSession); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	observed, err := ledgerAppObservedState(ctx, app, inventory, credential)
	if err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	if observed != ledgerAppTarget {
		return ledgerAppInventory{}, ledgerAdminCredential{}, ErrRecordLedgerFence
	}
	if err := ledgerCheckAppFenced(ctx, app, bootstrap, inventory, credential); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	if err := ledgerCheckAppPreparationSessions(ctx, app, bootstrapSession, inventory); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	if err := ledgerVerifyAdminCredential(ctx, app, request, address, credential); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	if err := ledgerCheckAppFenced(ctx, app, bootstrap, inventory, credential); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	if err := ledgerCheckAppPreparationSessions(ctx, app, bootstrapSession, inventory); err != nil {
		return ledgerAppInventory{}, ledgerAdminCredential{}, err
	}
	return inventory, credential, nil
}
