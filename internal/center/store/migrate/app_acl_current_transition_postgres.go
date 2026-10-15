package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	heartbeatPolicyMigrationPending             bool
	lifecycleMigrationPending                   bool
	settingsDeploymentOverridesMigrationPending bool
	settingsRowPresent                          bool
	incidentDefaults                            []byte
	settingsSnapshot                            []byte
	settingsExceptTransition                    []byte
	updatedAt                                   time.Time
	staleThreshold                              int64
}

func preflightAppACLCurrentTransitionInTx(
	ctx context.Context,
	tx pgx.Tx,
	transition appACLCurrentTransition,
) (appACLCurrentTransitionPreflight, error) {
	if tx == nil {
		return appACLCurrentTransitionPreflight{}, fmt.Errorf("registered APP transition preflight has no PostgreSQL transaction")
	}
	heartbeatPolicyMigrationPending, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition)
	if err != nil {
		return appACLCurrentTransitionPreflight{}, err
	}
	if heartbeatPolicyMigrationPending {
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
	} else {
		if err := verifyAppACLCurrentHeartbeatDefault(ctx, tx, appACLCurrentHeartbeatDefault); err != nil {
			return appACLCurrentTransitionPreflight{}, fmt.Errorf("verify registered APP transition predecessor current default: %w", err)
		}
		if err := verifyAppACLCurrentHeartbeatIndex(ctx, tx); err != nil {
			return appACLCurrentTransitionPreflight{}, fmt.Errorf("verify registered APP transition predecessor current index: %w", err)
		}
	}
	snapshot := appACLCurrentTransitionPreflight{
		heartbeatPolicyMigrationPending:             heartbeatPolicyMigrationPending,
		lifecycleMigrationPending:                   appACLCurrentTransitionContainsMigration(transition, "0067_refactor_vps_monitoring_lifecycle.sql"),
		settingsDeploymentOverridesMigrationPending: appACLCurrentTransitionContainsMigration(transition, "0075_add_settings_deployment_overrides.sql"),
	}
	err = tx.QueryRow(ctx, `
			select incident_defaults,
			       to_jsonb(settings),
			       to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[],
			       updated_at
			from public.center_settings settings
			where settings_id = 'center'
			for update
		`).Scan(&snapshot.incidentDefaults, &snapshot.settingsSnapshot, &snapshot.settingsExceptTransition, &snapshot.updatedAt)
	switch {
	case err == nil:
		snapshot.settingsRowPresent = true
	case errors.Is(err, pgx.ErrNoRows):
		return snapshot, nil
	default:
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
	var incidentDefaults, settingsSnapshot, settingsExceptTransition []byte
	var updatedAt time.Time
	err := tx.QueryRow(ctx, `
			select incident_defaults,
			       to_jsonb(settings),
			       to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[],
			       updated_at
			from public.center_settings settings
			where settings_id = 'center'
		`).Scan(&incidentDefaults, &settingsSnapshot, &settingsExceptTransition, &updatedAt)
	afterPresent := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read applied registered APP transition settings: %w", err)
	}
	return verifyAppliedAppACLCurrentTransitionSettings(before, afterPresent, incidentDefaults, settingsSnapshot, settingsExceptTransition, updatedAt)
}

func verifyAppliedAppACLCurrentTransitionSettings(
	before appACLCurrentTransitionPreflight,
	afterPresent bool,
	incidentDefaults, settingsSnapshot, settingsExceptTransition []byte,
	updatedAt time.Time,
) error {
	if before.settingsRowPresent != afterPresent {
		return fmt.Errorf("registered APP transition changed settings row presence")
	}
	if !before.settingsRowPresent {
		return nil
	}
	if before.settingsDeploymentOverridesMigrationPending {
		var err error
		before.settingsSnapshot, err = appACLCurrentSettingsDeploymentOverrides(before.settingsSnapshot)
		if err != nil {
			return err
		}
		before.settingsExceptTransition, err = appACLCurrentSettingsDeploymentOverrides(before.settingsExceptTransition)
		if err != nil {
			return err
		}
	}
	if before.lifecycleMigrationPending {
		var err error
		before.settingsSnapshot, err = appACLCurrentLifecycleSettings(before.settingsSnapshot)
		if err != nil {
			return err
		}
		before.settingsExceptTransition, err = appACLCurrentLifecycleSettings(before.settingsExceptTransition)
		if err != nil {
			return err
		}
	}
	if !before.heartbeatPolicyMigrationPending {
		if !appACLCurrentJSONEqual(settingsSnapshot, before.settingsSnapshot) {
			return fmt.Errorf("registered APP transition changed settings without a heartbeat policy migration")
		}
		return nil
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

// Match only the exact settings transformation performed by 0075 for an
// existing row. The migration's add-column default materializes true on old
// rows while changing the column default to false for future inserts.
func appACLCurrentSettingsDeploymentOverrides(payload []byte) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(payload, &settings); err != nil {
		return nil, fmt.Errorf("decode deployment override transition settings: %w", err)
	}
	if settings == nil {
		return nil, fmt.Errorf("deployment override transition settings must be an object")
	}
	settings["incident_sweep_runtime_managed"] = json.RawMessage("true")
	return json.Marshal(settings)
}

// Match only the exact settings transformation performed by 0067. Other
// settings remain part of the fail-closed predecessor/successor comparison.
func appACLCurrentLifecycleSettings(payload []byte) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(payload, &settings); err != nil {
		return nil, fmt.Errorf("decode lifecycle transition settings: %w", err)
	}
	for _, name := range []string{"retention_policy", "ip_quality_settings"} {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(settings[name], &values); err != nil {
			return nil, fmt.Errorf("decode lifecycle transition %s: %w", name, err)
		}
		if values == nil {
			return nil, fmt.Errorf("lifecycle transition %s must be an object", name)
		}
		if name == "retention_policy" {
			delete(values, "event_layer_days")
			delete(values, "notification_layer_days")
			values["raw_layer_days"] = json.RawMessage("30")
			values["aggregate_layer_days"] = json.RawMessage("365")
		} else {
			delete(values, "raw_retention_days")
			delete(values, "history_retention_days")
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		settings[name] = encoded
	}
	return json.Marshal(settings)
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
	if err := verifyAppACLCurrentIncidentSweepRuntimeManagedColumn(ctx, tx); err != nil {
		return fmt.Errorf("verify registered APP transition incident sweep ownership column: %w", err)
	}
	var incidentDefaults []byte
	err := tx.QueryRow(ctx, `
		select incident_defaults
		from public.center_settings
		where settings_id = 'center'
	`).Scan(&incidentDefaults)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
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

func verifyAppACLCurrentIncidentSweepRuntimeManagedColumn(ctx context.Context, tx pgx.Tx) error {
	var isBoolean, notNull bool
	var defaultExpression *string
	err := tx.QueryRow(ctx, `
		select attributes.atttypid = 'boolean'::regtype,
		       attributes.attnotnull,
		       pg_get_expr(defaults.adbin, defaults.adrelid)
		from pg_catalog.pg_attribute attributes
		join pg_catalog.pg_class relations
		  on relations.oid = attributes.attrelid
		join pg_catalog.pg_namespace namespaces
		  on namespaces.oid = relations.relnamespace
		left join pg_catalog.pg_attrdef defaults
		  on defaults.adrelid = attributes.attrelid
		 and defaults.adnum = attributes.attnum
		where namespaces.nspname = 'public'
		  and relations.relname = 'center_settings'
		  and attributes.attname = 'incident_sweep_runtime_managed'
		  and not attributes.attisdropped
	`).Scan(&isBoolean, &notNull, &defaultExpression)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("center_settings incident_sweep_runtime_managed column is missing")
	}
	if err != nil {
		return fmt.Errorf("read center_settings incident_sweep_runtime_managed column shape: %w", err)
	}
	if !isBoolean || !notNull || defaultExpression == nil || strings.TrimSpace(strings.ToLower(*defaultExpression)) != "false" {
		return fmt.Errorf("center_settings incident_sweep_runtime_managed column must be boolean not null default false")
	}
	return nil
}

func validateHeartbeatAppACLCurrentTransition(transition appACLCurrentTransition) error {
	_, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition)
	return err
}

func appACLCurrentTransitionContainsMigration(transition appACLCurrentTransition, migration string) bool {
	for _, name := range transition.successor.names {
		if name == migration {
			return true
		}
	}
	return false
}

func appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition appACLCurrentTransition) (bool, error) {
	// 0078 replaces only the family-state view definition. 0077 adds product
	// snapshot/cache tables and their runtime grants; 0076 adds the view and
	// SELECT grant, 0075 adds settings ownership, and 0072–0074 add schema only.
	// 0071 adds access-management ACL only, while 0070 adds nullable
	// destination-subject columns and 0069 adds CPU validity schema. These
	// non-heartbeat migrations must be stripped from the terminal suffix
	// before policy-shape classification.
	successorNames := transition.successor.names
	if transition.profile == appACLCurrentProfileP77 &&
		!equalAppACLCurrentMigrationNames(successorNames, []string{"0078_ip_quality_collection_scope.sql"}) {
		return false, fmt.Errorf("unsupported registered APP transition")
	}
	collectionScopeMigrationPending := false
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0078_ip_quality_collection_scope.sql" {
		collectionScopeMigrationPending = true
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0078_ip_quality_collection_scope.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	productMigrationPending := false
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0077_ip_quality_product.sql" {
		productMigrationPending = true
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0077_ip_quality_product.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	addressFamiliesMigrationPending := false
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0076_ip_quality_address_families.sql" {
		addressFamiliesMigrationPending = true
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0076_ip_quality_address_families.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	settingsDeploymentOverridesMigrationPending := false
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0075_add_settings_deployment_overrides.sql" {
		settingsDeploymentOverridesMigrationPending = true
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0075_add_settings_deployment_overrides.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0074_add_subscription_exchange_rate_month_end.sql" {
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0074_add_subscription_exchange_rate_month_end.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0073_add_monitoring_instance_metadata_version.sql" {
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0073_add_monitoring_instance_metadata_version.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0072_add_target_observation_freshness.sql" {
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0072_add_target_observation_freshness.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0071_add_access_management.sql" {
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0071_add_access_management.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0070_add_record_import_destination_subject.sql" {
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0070_add_record_import_destination_subject.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	cpuRatesMigrationPending := false
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0069_add_cpu_rates_valid.sql" {
		cpuRatesMigrationPending = true
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0069_add_cpu_rates_valid.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}
	addressIdentityMigrationPending := false
	if n := len(successorNames); n > 0 && successorNames[n-1] == "0068_normalize_ip_quality_host_address_identity.sql" {
		addressIdentityMigrationPending = true
		successorNames = successorNames[:n-1]
	}
	for _, name := range successorNames {
		if name == "0068_normalize_ip_quality_host_address_identity.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
	}

	lifecyclePending := appACLCurrentTransitionContainsMigration(transition, "0067_refactor_vps_monitoring_lifecycle.sql")
	if lifecyclePending {
		if !addressIdentityMigrationPending ||
			len(successorNames) == 0 ||
			successorNames[len(successorNames)-1] != "0067_refactor_vps_monitoring_lifecycle.sql" {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
		successorNames = successorNames[:len(successorNames)-1]
	}
	switch {
	case len(successorNames) == 0:
		// P69 through P77 carry only the stripped non-policy terminal suffix.
		if transition.profile == appACLCurrentProfileP77 && !collectionScopeMigrationPending {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
		if transition.profile == appACLCurrentProfileP76 && !productMigrationPending {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
		if transition.profile == appACLCurrentProfileP75 && !addressFamiliesMigrationPending {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
		if transition.profile == appACLCurrentProfileP74 && !settingsDeploymentOverridesMigrationPending {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
		if transition.profile == appACLCurrentProfileP69 ||
			transition.profile == appACLCurrentProfileP70 ||
			transition.profile == appACLCurrentProfileP71 ||
			transition.profile == appACLCurrentProfileP72 ||
			transition.profile == appACLCurrentProfileP73 ||
			transition.profile == appACLCurrentProfileP74 ||
			transition.profile == appACLCurrentProfileP75 ||
			transition.profile == appACLCurrentProfileP76 ||
			transition.profile == appACLCurrentProfileP77 {
			return false, nil
		}
		if !addressIdentityMigrationPending && !cpuRatesMigrationPending {
			return false, fmt.Errorf("unsupported registered APP transition")
		}
		return false, nil
	case len(successorNames) == 4 &&
		lifecyclePending &&
		addressIdentityMigrationPending &&
		successorNames[0] == "0063_tune_heartbeat_incident_policy.sql" &&
		successorNames[1] == "0064_add_network_rates_valid.sql" &&
		successorNames[2] == "0065_extend_vps_lifecycle_audit_and_snapshot.sql" &&
		successorNames[3] == "0066_constrain_monitoring_and_target_state_values.sql":
		return true, nil
	case len(successorNames) == 3 &&
		lifecyclePending &&
		addressIdentityMigrationPending &&
		successorNames[0] == "0064_add_network_rates_valid.sql" &&
		successorNames[1] == "0065_extend_vps_lifecycle_audit_and_snapshot.sql" &&
		successorNames[2] == "0066_constrain_monitoring_and_target_state_values.sql":
		return false, nil
	case len(successorNames) == 2 &&
		lifecyclePending &&
		addressIdentityMigrationPending &&
		successorNames[0] == "0065_extend_vps_lifecycle_audit_and_snapshot.sql" &&
		successorNames[1] == "0066_constrain_monitoring_and_target_state_values.sql":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported registered APP transition")
	}
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
