//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
	"reflect"

	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/store/migrate"
)

// These groups are the seven actual exports at frozen C80 ec38a48d. They must
// be compared with the pinned helper during build acceptance, never regenerated
// from the current successor's surface exports.
var c80InventorySurfaces = [...][]string{
	{"content_delivery_epochs", "record_core_purge_receipts", "record_domain_activities", "record_draft_checkpoints", "record_drafts", "record_revision_participants", "record_revision_subjects", "record_revision_tags", "record_revisions", "records"},
	{"attachment_processor_jobs", "attachment_purge_receipts", "attachment_quota_accounts", "attachment_upload_parts", "attachment_uploads", "blob_gc_deletions", "blob_gc_pins", "blob_objects", "blob_publication_intents", "content_processor_workspaces", "content_workspace_purge_receipts", "record_attachments", "record_revision_attachments"},
	{"evidence_capture_intents", "evidence_copy_lineage", "evidence_payload_gc_receipts", "evidence_payloads", "evidence_purge_receipts", "evidence_snapshots", "record_revision_evidence"},
	{"record_search_documents", "record_search_purge_receipts", "record_search_subjects"},
	{"record_activity_projection", "record_activity_purge_receipts", "record_activity_revision_intervals", "record_activity_subjects"},
	{"record_action_events", "record_actions", "record_collaboration_purge_receipts", "record_comment_mentions", "record_comment_replies", "record_comment_revisions", "record_comment_tombstones", "record_comments", "record_followers", "record_notification_audit_summaries", "record_notification_deliveries", "record_notification_delivery_attempts", "record_notification_recipients", "record_notifications"},
	{"record_export_artifacts", "record_export_jobs", "record_import_artifacts", "record_import_entity_mappings", "record_import_jobs", "record_import_plans", "record_origin_tombstones", "record_origins", "record_portability_purge_receipts"},
}

var c80InventoryProjections = [...]string{"client_content_leases", "backup_epochs", "recovery_inventory_projection"}

// The complete source graph, live policies, activation, and tombstones are
// observed without acquiring the child-held source graph advisory lock.
var c80InventorySourceTables = [...]string{
	"vps_assets", "monitoring_instances", "targets", "vps_monitoring_instance_links",
	"asset_service_associations", "asset_domain_associations", "vps_maintenance_actions",
	"vps_maintenance_effects", "source_deletion_tombstones", "deployment_contract_state",
}

var c80InventoryMustBeEmpty = [...]string{
	"client_content_leases", "backup_epochs", "recovery_inventory_projection",
	"evidence_snapshots", "evidence_payloads", "evidence_copy_lineage", "record_revision_evidence",
	"evidence_capture_intents", "attachment_processor_jobs", "content_processor_workspaces",
	"record_export_jobs", "record_export_artifacts", "record_import_jobs", "record_import_plans",
	"record_import_artifacts", "record_import_entity_mappings",
}

const c80InventoryRowLimit = 4096
const c80InventoryByteLimit = 8 << 20

type legacyInventoryObservation struct {
	Op               string  `json:"op"`
	PhaseGeneration  uint64  `json:"phase_generation"`
	Available        bool    `json:"available"`
	RealmDigest      *string `json:"realm_digest"`
	CatalogDigest    *string `json:"catalog_digest"`
	BackupDigest     *string `json:"backup_digest"`
	ClientDigest     *string `json:"client_digest"`
	ComparisonDigest *string `json:"comparison_digest"`
	DependencyDigest *string `json:"dependency_digest"`
	ProcessorDigest  *string `json:"processor_digest"`
}

type ledgerC80TableObservation struct {
	name   string
	count  uint64
	digest [32]byte
}

func legacyInventoryField(writer hash.Hash, body []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(body)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(body)
}

func legacyInventoryNumber(value uint64) [8]byte {
	var body [8]byte
	binary.BigEndian.PutUint64(body[:], value)
	return body
}

func legacyInventoryDigest(domain string, fields ...[]byte) [32]byte {
	writer := sha256.New()
	legacyInventoryField(writer, []byte(domain))
	for _, field := range fields {
		legacyInventoryField(writer, field)
	}
	var result [32]byte
	writer.Sum(result[:0])
	return result
}

func ledgerC80Table(ctx context.Context, tx pgx.Tx, name string, bytesSeen *int) (ledgerC80TableObservation, error) {
	result := ledgerC80TableObservation{name: name}
	// Identifiers come exclusively from the fixed arrays above. The same RR
	// snapshot has already admitted their exact C80 catalog and physical kind.
	rows, err := tx.Query(ctx, `select pg_catalog.to_jsonb(r)::text, count(*) over () from `+pgx.Identifier{"public", name}.Sanitize()+` r order by pg_catalog.to_jsonb(r)::text collate "C" limit 4097`)
	if err != nil {
		return result, errLedgerInventoryUnavailable
	}
	defer rows.Close()
	writer := sha256.New()
	legacyInventoryField(writer, []byte("HOUFENG-C80-TABLE-V1"))
	legacyInventoryField(writer, []byte(name))
	var observed uint64
	for rows.Next() {
		var body string
		var count int64
		if rows.Scan(&body, &count) != nil || count <= 0 || count > c80InventoryRowLimit || observed >= c80InventoryRowLimit {
			return result, errLedgerInventoryUnavailable
		}
		if observed == 0 {
			result.count = uint64(count)
			encoded := legacyInventoryNumber(result.count)
			legacyInventoryField(writer, encoded[:])
		} else if uint64(count) != result.count {
			return result, errLedgerInventoryUnavailable
		}
		if len(body) > c80InventoryByteLimit-*bytesSeen {
			return result, errLedgerInventoryUnavailable
		}
		*bytesSeen += len(body)
		legacyInventoryField(writer, []byte(body))
		observed++
	}
	if rows.Err() != nil || observed != result.count {
		return result, errLedgerInventoryUnavailable
	}
	if observed == 0 {
		zero := legacyInventoryNumber(0)
		legacyInventoryField(writer, zero[:])
	}
	writer.Sum(result.digest[:0])
	return result, nil
}

func ledgerC80TupleDigest(domain string, base [32]byte, roots [32]byte, tables []ledgerC80TableObservation) [32]byte {
	writer := sha256.New()
	legacyInventoryField(writer, []byte(domain))
	legacyInventoryField(writer, base[:])
	legacyInventoryField(writer, roots[:])
	count := legacyInventoryNumber(uint64(len(tables)))
	legacyInventoryField(writer, count[:])
	for _, table := range tables {
		legacyInventoryField(writer, []byte(table.name))
		rows := legacyInventoryNumber(table.count)
		legacyInventoryField(writer, rows[:])
		legacyInventoryField(writer, table.digest[:])
	}
	var result [32]byte
	writer.Sum(result[:0])
	return result
}

// ledgerReadC80Inventory consumes an already held callback's exact journal
// state. It never opens a pool, commits, changes a role, or creates authority.
// The caller must bracket this transaction with fresh matching live root
// observations and must reject a failed commit before returning any digest.
func ledgerReadC80Inventory(ctx context.Context, tx pgx.Tx, inventory ledgerAppInventory, roles ledgerAppRoleSnapshot, phase uint64, realm ledgerRealmObservation, sessions map[int32]ledgerAppSession) (legacyInventoryObservation, error) {
	unavailable := legacyInventoryObservation{Op: "inventory_observe", PhaseGeneration: phase}
	if ctx == nil || tx == nil || phase == 0 || !inventory.LegacyC80 || realm.Realm == ([32]byte{}) || realm.Roots == ([32]byte{}) {
		return unavailable, ErrRecordLedgerFence
	}
	actual, err := migrate.ReadAppACLMaintenanceSnapshotInTransaction(ctx, tx, nil)
	if err != nil || !actual.LegacyC80 || !reflect.DeepEqual(actual.Manifest, inventory.Baseline.Manifest) || !reflect.DeepEqual(actual.Catalog, inventory.Baseline.Catalog) {
		return unavailable, errLedgerInventoryUnavailable
	}
	if ledgerAppIdentity(ctx, tx, inventory.Application) != nil {
		return unavailable, ErrRecordLedgerFence
	}
	observedRoles, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(observedRoles, roles) {
		return unavailable, ErrRecordLedgerFence
	}
	if err := ledgerCheckAppSessions(ctx, tx, inventory, sessions); err != nil {
		return unavailable, err
	}
	var prepared int
	if tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, ledgerAppRoleNames(inventory)).Scan(&prepared) != nil || prepared != 0 {
		return unavailable, ErrRecordLedgerFence
	}
	for _, statement := range []string{`set local TimeZone='UTC'`, `set local DateStyle='ISO, YMD'`, `set local bytea_output='hex'`} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return unavailable, errLedgerInventoryUnavailable
		}
	}
	var names []string
	for _, group := range c80InventorySurfaces {
		names = append(names, group...)
	}
	names = append(names, c80InventoryProjections[:]...)
	names = append(names, c80InventorySourceTables[:]...)
	var physical int
	if tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace where n.nspname='public' and c.relname=any($1::text[]) and c.relkind='r' and not c.relispartition`, names).Scan(&physical) != nil || physical != len(names) {
		return unavailable, errLedgerInventoryUnavailable
	}
	tables := make([]ledgerC80TableObservation, 0, len(names))
	byName := make(map[string]ledgerC80TableObservation, len(names))
	bytesSeen := 0
	for _, name := range names {
		table, err := ledgerC80Table(ctx, tx, name, &bytesSeen)
		if err != nil {
			return unavailable, err
		}
		tables = append(tables, table)
		byName[name] = table
	}
	for _, name := range c80InventoryMustBeEmpty {
		if byName[name].count != 0 {
			return unavailable, errLedgerInventoryUnavailable
		}
	}
	catalogBody, err := json.Marshal(actual)
	if err != nil {
		return unavailable, ErrRecordLedgerFence
	}
	catalog := legacyInventoryDigest("HOUFENG-C80-CATALOG-V1", catalogBody)
	appBody, err := json.Marshal(inventory.Application)
	if err != nil {
		return unavailable, ErrRecordLedgerFence
	}
	generation := legacyInventoryNumber(phase)
	base := legacyInventoryDigest("HOUFENG-C80-INVENTORY-BINDING-V1", realm.Realm[:], appBody, generation[:], catalog[:])
	dependency := ledgerC80TupleDigest("HOUFENG-C80-DEPENDENCY-V1", base, realm.Roots, tables)
	backup := ledgerC80TupleDigest("HOUFENG-C80-BACKUP-V1", base, realm.Roots, []ledgerC80TableObservation{byName["backup_epochs"], byName["recovery_inventory_projection"]})
	client := ledgerC80TupleDigest("HOUFENG-C80-CLIENT-V1", base, realm.Roots, []ledgerC80TableObservation{byName["client_content_leases"]})
	comparisonBase := legacyInventoryDigest("HOUFENG-C80-COMPARISON-V1", base[:], client[:])
	comparison := ledgerC80TupleDigest("HOUFENG-C80-COMPARISON-V1", comparisonBase, realm.Roots, []ledgerC80TableObservation{byName["evidence_snapshots"], byName["evidence_payloads"], byName["evidence_copy_lineage"], byName["record_revision_evidence"], byName["evidence_capture_intents"]})
	processor := ledgerC80TupleDigest("HOUFENG-C80-PROCESSOR-V1", base, realm.Roots, []ledgerC80TableObservation{byName["attachment_processor_jobs"], byName["content_processor_workspaces"]})
	if err := ledgerCheckAppSessions(ctx, tx, inventory, sessions); err != nil {
		return unavailable, err
	}
	text := func(digest [32]byte) *string { value := hex.EncodeToString(digest[:]); return &value }
	return legacyInventoryObservation{Op: "inventory_observe", PhaseGeneration: phase, Available: true, RealmDigest: text(realm.Realm), CatalogDigest: text(catalog), BackupDigest: text(backup), ClientDigest: text(client), ComparisonDigest: text(comparison), DependencyDigest: text(dependency), ProcessorDigest: text(processor)}, nil
}
