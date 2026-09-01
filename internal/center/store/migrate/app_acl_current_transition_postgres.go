package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const appACLCurrentHeartbeatIndexName = "idx_monitoring_instance_heartbeats_live_received"

const appACLCurrentHeartbeatDefaultV0794 = `{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}`
const appACLCurrentHeartbeatDefault = `{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}`

type appACLCurrentTransitionPreflight struct {
	incidentDefaults         []byte
	settingsExceptTransition []byte
	updatedAt                time.Time
	staleThreshold           int64
}

func preflightAppACLCurrentTransitionInTx(
	ctx context.Context,
	tx pgx.Tx,
	transition appACLCurrentTransition,
) (appACLCurrentTransitionPreflight, error) {
	if tx == nil {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("registered APP transition preflight has no PostgreSQL transaction")
	}
	if err := validateHeartbeatAppACLCurrentTransition(transition); err != nil {
		return appACLCurrentTransitionPreflight{}, err
	}
	var indexAbsent bool
	if err := tx.QueryRow(ctx, `select to_regclass('public.idx_monitoring_instance_heartbeats_live_received') is null`).Scan(&indexAbsent); err != nil {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("read registered APP transition predecessor index state: %w", err)
	}
	if !indexAbsent {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("registered APP transition predecessor already has reserved heartbeat index")
	}
	if err := verifyAppACLCurrentHeartbeatDefault(ctx, tx, appACLCurrentHeartbeatDefaultV0794); err != nil {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("verify registered APP transition predecessor default: %w", err)
	}
	var snapshot appACLCurrentTransitionPreflight
	if err := tx.QueryRow(ctx, `
			select incident_defaults,
			       to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[],
			       updated_at
			from public.center_settings settings
			where settings_id = 'center'
			for update
		`).Scan(&snapshot.incidentDefaults, &snapshot.settingsExceptTransition, &snapshot.updatedAt); err != nil {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("read registered APP transition settings snapshot: %w", err)
	}
	threshold, err := appACLCurrentStaleThreshold(snapshot.incidentDefaults)
	if err != nil {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("decode registered APP transition settings snapshot: %w", err)
	}
	snapshot.staleThreshold = threshold
	return snapshot, nil
}

func verifyAppliedAppACLCurrentTransitionInTx(
	ctx context.Context,
	tx pgx.Tx,
	transition appACLCurrentTransition,
	before appACLCurrentTransitionPreflight,
) error {
	if err := verifyCurrentAppACLCurrentTransitionInTx(ctx, tx, transition); err != nil {
		return err
	}
	var incidentDefaults, settingsExceptTransition []byte
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
			select incident_defaults,
			       to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[],
			       updated_at
			from public.center_settings settings
			where settings_id = 'center'
		`).Scan(&incidentDefaults, &settingsExceptTransition, &updatedAt); err != nil {
		return fmt.Errorf("read applied registered APP transition settings: %w", err)
	}
	if !appACLCurrentJSONEqual(settingsExceptTransition, before.settingsExceptTransition) {
		return fmt.Errorf("registered APP transition changed non-incident settings")
	}
	if before.staleThreshold == 3 {
		want, err := appACLCurrentJSONWithStaleThreshold(before.incidentDefaults, 12)
		if err != nil {
			return fmt.Errorf("build registered APP transition expected settings: %w", err)
		}
		if !appACLCurrentJSONEqual(incidentDefaults, want) {
			return fmt.Errorf("registered APP transition did not change only the default stale threshold")
		}
		if !updatedAt.After(before.updatedAt) {
			return fmt.Errorf("registered APP transition did not advance settings updated_at")
		}
		return nil
	}
	if !appACLCurrentJSONEqual(incidentDefaults, before.incidentDefaults) || !updatedAt.Equal(before.updatedAt) {
		return fmt.Errorf("registered APP transition changed custom incident defaults")
	}
	return nil
}

func verifyCurrentAppACLCurrentTransitionInTx(
	ctx context.Context,
	tx pgx.Tx,
	transition appACLCurrentTransition,
) error {
	if tx == nil {
		return fmt.Errorf("registered APP transition verifier has no PostgreSQL transaction")
	}
	if err := validateHeartbeatAppACLCurrentTransition(transition); err != nil {
		return err
	}
	if err := verifyAppACLCurrentHeartbeatDefault(ctx, tx, appACLCurrentHeartbeatDefault); err != nil {
		return fmt.Errorf("verify registered APP transition current default: %w", err)
	}
	if err := verifyAppACLCurrentHeartbeatIndex(ctx, tx); err != nil {
		return err
	}
	var incidentDefaults []byte
	if err := tx.QueryRow(ctx, `
		select incident_defaults
		from public.center_settings
		where settings_id = 'center'
	`).Scan(&incidentDefaults); err != nil {
		return fmt.Errorf("read current registered APP transition settings: %w", err)
	}
	threshold, err := appACLCurrentStaleThreshold(incidentDefaults)
	if err != nil {
		return fmt.Errorf("decode current registered APP transition settings: %w", err)
	}
	if threshold < 1 {
		return fmt.Errorf("current registered APP transition stale threshold is invalid")
	}
	return nil
}

func validateHeartbeatAppACLCurrentTransition(transition appACLCurrentTransition) error {
	if len(transition.successor.names) != 1 || transition.successor.names[0] != "0063_tune_heartbeat_incident_policy.sql" ||
		len(transition.predecessor.sources.names) != 63 ||
		transition.predecessor.sources.names[62] != "0062_create_vps_create_idempotency.sql" ||
		transition.predecessorManifestDigest != appACLCurrentV0794ManifestDigestGolden {
		return fmt.Errorf("unsupported registered APP transition")
	}
	return nil
}

func verifyAppACLCurrentHeartbeatDefault(ctx context.Context, tx pgx.Tx, expectedJSON string) error {
	var matches bool
	if err := tx.QueryRow(ctx, `
		select pg_get_expr(defaults.adbin, defaults.adrelid) = format('%L::jsonb', $1::jsonb)
		from pg_catalog.pg_attrdef defaults
		join pg_catalog.pg_class relations on relations.oid = defaults.adrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = relations.relnamespace
		join pg_catalog.pg_attribute attributes
		  on attributes.attrelid = relations.oid
		 and attributes.attnum = defaults.adnum
		where namespaces.nspname = 'public'
		  and relations.relname = 'center_settings'
		  and attributes.attname = 'incident_defaults'
	`, expectedJSON).Scan(&matches); err != nil {
		return fmt.Errorf("read center_settings incident_defaults column default: %w", err)
	}
	if !matches {
		return fmt.Errorf("center_settings incident_defaults column default does not match registered shape")
	}
	return nil
}

func verifyAppACLCurrentHeartbeatIndex(ctx context.Context, tx pgx.Tx) error {
	var valid, ready, unique bool
	var accessMethod, predicate string
	var keyCount, attributeCount int16
	var attributes []string
	var options []int16
	if err := tx.QueryRow(ctx, `
		select indexes.indisvalid,
		       indexes.indisready,
		       indexes.indisunique,
		       methods.amname,
		       indexes.indnkeyatts,
		       indexes.indnatts,
		       array(
		         select attributes.attname
		         from unnest(indexes.indkey::smallint[]) with ordinality keys(attnum, ordinal)
		         join pg_catalog.pg_attribute attributes
		           on attributes.attrelid = indexes.indrelid and attributes.attnum = keys.attnum
		         order by keys.ordinal
		       ),
		       indexes.indoption::smallint[],
		       pg_get_expr(indexes.indpred, indexes.indrelid)
		from pg_catalog.pg_index indexes
		join pg_catalog.pg_class index_rel on index_rel.oid = indexes.indexrelid
		join pg_catalog.pg_class table_rel on table_rel.oid = indexes.indrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = table_rel.relnamespace
		join pg_catalog.pg_am methods on methods.oid = index_rel.relam
		where namespaces.nspname = 'public'
		  and table_rel.relname = 'monitoring_instance_heartbeats'
		  and index_rel.relname = 'idx_monitoring_instance_heartbeats_live_received'
	`).Scan(&valid, &ready, &unique, &accessMethod, &keyCount, &attributeCount, &attributes, &options, &predicate); err != nil {
		return fmt.Errorf("read registered APP transition heartbeat index: %w", err)
	}
	wantAttributes := []string{"monitoring_instance_id", "received_at", "id", "sync_batch_id"}
	wantOptions := []int16{0, 3, 3}
	if !valid || !ready || unique || accessMethod != "btree" || keyCount != 3 || attributeCount != 4 ||
		!reflect.DeepEqual(attributes, wantAttributes) || !reflect.DeepEqual(options, wantOptions) ||
		strings.Join(strings.Fields(predicate), " ") != "(is_backfilled = false)" {
		return fmt.Errorf("registered APP transition heartbeat index does not match exact current shape")
	}
	return nil
}

func appACLCurrentStaleThreshold(body []byte) (int64, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return 0, err
	}
	raw, ok := object["stale_threshold_intervals"]
	if !ok {
		return 0, fmt.Errorf("stale_threshold_intervals is absent")
	}
	var threshold int64
	if err := json.Unmarshal(raw, &threshold); err != nil {
		return 0, err
	}
	return threshold, nil
}

func appACLCurrentJSONWithStaleThreshold(body []byte, threshold int64) ([]byte, error) {
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	object["stale_threshold_intervals"] = threshold
	return json.Marshal(object)
}

func appACLCurrentJSONEqual(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return bytes.Equal(left, right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
