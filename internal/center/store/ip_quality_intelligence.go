package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

var _ ipquality.IntelligenceRepository = (*PostgresIPQualityRepository)(nil)

// Every intelligence write takes locks in this order: raw settings, report and
// its ownership rows, job. Network work never holds any of these locks.
func lockIPQualityIntelligenceSettings(ctx context.Context, tx pgx.Tx) (centersettings.IPQualitySettings, error) {
	args, err := settingsWriteArgs(centersettings.FreshInstallDefaults())
	if err != nil {
		return centersettings.IPQualitySettings{}, err
	}
	if _, err = tx.Exec(ctx, insertDefaultCenterSettingsSQL, args...); err != nil {
		return centersettings.IPQualitySettings{}, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `select ip_quality_settings from center_settings where settings_id=$1 for share`, centersettings.SingletonID).Scan(&raw); err != nil {
		return centersettings.IPQualitySettings{}, err
	}
	settings := centersettings.Default().IPQuality
	if err = decodeSettingsJSON(raw, &settings); err != nil {
		return centersettings.IPQualitySettings{}, fmt.Errorf("decode intelligence settings")
	}
	return settings, nil
}

func lockIPQualityIntelligenceReport(ctx context.Context, tx pgx.Tx, reportID string) (ipquality.ReportWrite, bool, error) {
	var report ipquality.ReportWrite
	var product []byte
	err := tx.QueryRow(ctx, `select monitoring_instance_id, fingerprint, observed_at, received_at, ip_address, ip_version, status, asn, product_json
 from ip_quality_reports where report_id=$1 for share`, reportID).Scan(&report.MonitoringInstanceID, &report.Fingerprint, &report.ObservedAt, &report.ReceivedAt, &report.IPAddress, &report.IPVersion, &report.Status, &report.ASN, &product)
	if err != nil {
		return report, false, err
	}
	report.Product, err = decodeIPQualityAgentProduct(product)
	if err != nil {
		return report, false, err
	}
	var vpsID, lifecycle, monitoring, binding, fingerprint, vpsLifecycle string
	var archived *time.Time
	err = tx.QueryRow(ctx, `select n.vps_id,n.lifecycle_status,n.monitoring_status,n.binding_status,coalesce(n.binding_fingerprint,''),n.archived_at,v.lifecycle_status
 from monitoring_instances n join vps_assets v on v.vps_id=n.vps_id
 where n.monitoring_instance_id=$1 for share of n,v`, report.MonitoringInstanceID).Scan(&vpsID, &lifecycle, &monitoring, &binding, &fingerprint, &archived, &vpsLifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return report, false, nil
	}
	if err != nil {
		return report, false, err
	}
	rows, err := tx.Query(ctx, `select vps_id,unlinked_at from vps_monitoring_instance_links where monitoring_instance_id=$1 order by link_id for share`, report.MonitoringInstanceID)
	if err != nil {
		return report, false, err
	}
	linked := false
	for rows.Next() {
		var owner string
		var unlinked *time.Time
		if err = rows.Scan(&owner, &unlinked); err != nil {
			rows.Close()
			return report, false, err
		}
		if owner == vpsID && unlinked == nil {
			linked = true
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return report, false, err
	}
	eligible := linked && archived == nil && lifecycle != monitoringinstances.LifecycleRetired && lifecycle != monitoringinstances.LifecyclePendingEnrollment && vpsLifecycle != "archived" && monitoring != monitoringinstances.MonitoringPaused && binding == monitoringinstances.BindingBound && fingerprint != "" && fingerprint == report.Fingerprint && report.Product != nil && report.Product.Revision == 1
	return report, eligible, nil
}

func (r *PostgresIPQualityRepository) ClaimIPQualityIntelligence(ctx context.Context, ownerID string) (*ipquality.IntelligenceClaim, error) {
	if r.pool == nil || strings.TrimSpace(ownerID) == "" || len(ownerID) > 256 {
		return nil, fmt.Errorf("invalid intelligence repository or owner")
	}
	// Cache cleanup never removes an active quota restriction.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockIPQualityIntelligenceSettings(ctx, tx); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `delete from ip_quality_source_cache where expires_at < clock_timestamp()-interval '24 hours' and (blocked_until is null or blocked_until <= clock_timestamp())`); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	// Discovery is deliberately unlocked: locking a job before its ownership
	// would invert finalize's lock order. The per-candidate transaction rechecks
	// everything and SKIP LOCKED excludes a concurrent owner.
	// Bound each discovery pass even when the backlog contains many expired
	// jobs. Later passes continue oldest-first cleanup without retaining it all.
	rows, err := r.pool.Query(ctx, `select report_id from ip_quality_intelligence
 where state in ('pending','running') and (deadline_at<=clock_timestamp() or state='pending' or lease_until is null or lease_until<=clock_timestamp())
 order by (deadline_at<=clock_timestamp() or attempt_count>=3) desc,created_at,report_id limit 64`)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, 64)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		claim, err := r.claimIPQualityIntelligenceCandidate(ctx, id, ownerID)
		if err != nil {
			return nil, err
		}
		if claim != nil {
			return claim, nil
		}
	}
	return nil, nil
}

func (r *PostgresIPQualityRepository) claimIPQualityIntelligenceCandidate(ctx context.Context, id, owner string) (*ipquality.IntelligenceClaim, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	settings, err := lockIPQualityIntelligenceSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	report, eligible, err := lockIPQualityIntelligenceReport(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state string
	var deadline, now time.Time
	var lease *time.Time
	var attempts int
	err = tx.QueryRow(ctx, `select state,deadline_at,lease_until,attempt_count,clock_timestamp() from ip_quality_intelligence where report_id=$1 for update skip locked`, id).Scan(&state, &deadline, &lease, &attempts, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state != "pending" && state != "running" {
		return nil, nil
	}
	live := state == "running" && lease != nil && lease.After(now)
	if !deadline.After(now) || (attempts >= 3 && !live) {
		_, err = tx.Exec(ctx, `update ip_quality_intelligence set state='failed',completed_at=clock_timestamp(),owner_generation=owner_generation+1,owner_id=null,lease_until=null,error_code='deadline_or_attempts_exhausted' where report_id=$1`, id)
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}
	if live {
		return nil, nil
	}
	if !settings.Enabled || !eligible {
		_, err = tx.Exec(ctx, `update ip_quality_intelligence set state='skipped',completed_at=clock_timestamp(),owner_generation=owner_generation+1,owner_id=null,lease_until=null,error_code='lifecycle_changed' where report_id=$1`, id)
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}
	claim := &ipquality.IntelligenceClaim{ReportID: id, OwnerID: owner, Report: report}
	err = tx.QueryRow(ctx, `update ip_quality_intelligence set state='running',owner_id=$2,owner_generation=owner_generation+1,attempt_count=attempt_count+1,started_at=clock_timestamp(),lease_until=least(clock_timestamp()+interval '120 seconds',deadline_at),error_code='' where report_id=$1 returning owner_generation,attempt_count,deadline_at,lease_until`, id, owner).Scan(&claim.OwnerGeneration, &claim.AttemptCount, &claim.DeadlineAt, &claim.LeaseUntil)
	if err != nil {
		return nil, err
	}
	if err = loadIPQualityIntelligenceChildren(ctx, tx, claim); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return claim, nil
}

func loadIPQualityIntelligenceChildren(ctx context.Context, tx pgx.Tx, claim *ipquality.IntelligenceClaim) error {
	providers, err := queryIPQualityProductProviders(ctx, tx, []string{claim.ReportID})
	if err != nil {
		return err
	}
	claim.Report.ProviderResults = providers[claim.ReportID]
	services, err := serviceUnlocksForReport(ctx, tx, claim.ReportID)
	if err != nil {
		return err
	}
	for _, s := range services {
		claim.Report.ServiceUnlocks = append(claim.Report.ServiceUnlocks, ipquality.ServiceUnlockWrite{Service: s.Service, Status: s.Status, ProbeStatus: s.ProbeStatus, UnlockType: s.UnlockType, Checks: s.Checks})
	}
	return nil
}

func (r *PostgresIPQualityRepository) IPQualityIntelligenceEligible(ctx context.Context, claim ipquality.IntelligenceClaim) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	settings, err := lockIPQualityIntelligenceSettings(ctx, tx)
	if err != nil {
		return false, err
	}
	_, eligible, err := lockIPQualityIntelligenceReport(ctx, tx, claim.ReportID)
	if err != nil {
		return false, err
	}
	var owned bool
	err = tx.QueryRow(ctx, `select state='running' and owner_id=$2 and owner_generation=$3 and lease_until>clock_timestamp() and deadline_at>clock_timestamp() from ip_quality_intelligence where report_id=$1`, claim.ReportID, claim.OwnerID, claim.OwnerGeneration).Scan(&owned)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return settings.Enabled && eligible && owned, err
}

func intelligenceFingerprint(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
func intelligenceCredentials(settings centersettings.IPQualitySettings) map[string]string {
	return map[string]string{
		"ipapi.is":  intelligenceFingerprint(settings.Sources.IPAPIISKey),
		"abuseipdb": intelligenceFingerprint(settings.Sources.AbuseIPDBKey),
		"maxmind":   centersettings.MaxMindCredentialFingerprint(settings.Sources.MaxMindAccountID, settings.Sources.MaxMindLicenseKey),
	}
}

func (r *PostgresIPQualityRepository) CompleteIPQualityIntelligence(ctx context.Context, claim ipquality.IntelligenceClaim, result ipquality.IntelligenceSnapshot) error {
	if r.pool == nil {
		return fmt.Errorf("invalid intelligence repository")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	settings, err := lockIPQualityIntelligenceSettings(ctx, tx)
	if err != nil {
		return err
	}
	report, eligible, err := lockIPQualityIntelligenceReport(ctx, tx, claim.ReportID)
	if err != nil {
		return err
	}
	var owned bool
	err = tx.QueryRow(ctx, `select state='running' and owner_id=$2 and owner_generation=$3 and lease_until>clock_timestamp() and deadline_at>clock_timestamp() from ip_quality_intelligence where report_id=$1 for update`, claim.ReportID, claim.OwnerID, claim.OwnerGeneration).Scan(&owned)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !owned) {
		return ipquality.ErrIntelligenceOwnership
	}
	if err != nil {
		return err
	}
	errorCode := ""
	if !settings.Enabled || !eligible {
		result = ipquality.IntelligenceSnapshot{State: "skipped"}
		errorCode = "lifecycle_changed"
	} else {
		// A collector skipped state is not lifecycle proof. An eligibility read
		// error cancels further requests; collected facts stay partial or failed.
		if result.State == "skipped" {
			result.State = ""
		}
		revokeIPQualityIntelligenceCredentials(&result, intelligenceCredentials(settings))
		if err = ipquality.ValidateIntelligenceSnapshot(result, report.IPAddress); err != nil {
			return fmt.Errorf("invalid intelligence snapshot")
		}
	}
	// Read immutable Agent facts again; caller-owned claim data is not trusted.
	fresh := ipquality.IntelligenceClaim{ReportID: claim.ReportID, Report: report}
	if err = loadIPQualityIntelligenceChildren(ctx, tx, &fresh); err != nil {
		return err
	}
	state := intelligenceTerminalState(result)
	data, err := json.Marshal(result)
	if err != nil || len(data) > 512<<10 {
		return fmt.Errorf("invalid intelligence snapshot size")
	}
	summary, err := json.Marshal(ipquality.BuildProductView(fresh.Report, &result).Summary)
	if err != nil {
		return fmt.Errorf("encode intelligence summary")
	}
	for _, entry := range result.CacheWrites {
		if err = validateIPQualityIntelligenceCache(entry, result, settings); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into ip_quality_source_cache(source_id,resource,credential_fingerprint,fetched_at,expires_at,blocked_until,payload_json)
   values($1,$2,$3,$4,$5,$6,$7::jsonb) on conflict(source_id,resource,credential_fingerprint) do update set
    fetched_at=greatest(ip_quality_source_cache.fetched_at,excluded.fetched_at),
    expires_at=case when excluded.resource='@quota' then greatest(ip_quality_source_cache.expires_at,excluded.expires_at) else excluded.expires_at end,
    blocked_until=case when excluded.resource='@quota' then greatest(ip_quality_source_cache.blocked_until,excluded.blocked_until) else excluded.blocked_until end,
    payload_json=excluded.payload_json
   where excluded.resource='@quota' or excluded.fetched_at>=ip_quality_source_cache.fetched_at`, entry.SourceID, entry.Resource, entry.CredentialFingerprint, entry.FetchedAt, entry.ExpiresAt, entry.BlockedUntil, []byte(entry.PayloadJSON)); err != nil {
			return err
		}
	}
	// Check real DB time once more after all validation/cache work. A failure
	// rolls the whole transaction back, including cache writes.
	tag, err := tx.Exec(ctx, `update ip_quality_intelligence set state=$4,completed_at=clock_timestamp(),data_json=$5::jsonb,summary_json=$6::jsonb,error_code=$7,owner_id=null,lease_until=null
 where report_id=$1 and state='running' and owner_id=$2 and owner_generation=$3 and lease_until>clock_timestamp() and deadline_at>clock_timestamp()`, claim.ReportID, claim.OwnerID, claim.OwnerGeneration, state, data, summary, errorCode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ipquality.ErrIntelligenceOwnership
	}
	return tx.Commit(ctx)
}

func revokeIPQualityIntelligenceCredentials(result *ipquality.IntelligenceSnapshot, current map[string]string) {
	revoked := map[string]bool{}
	for source, fingerprint := range result.CredentialFingerprints {
		if current[source] == "" || current[source] != fingerprint {
			revoked[source] = true
		}
	}
	for _, provider := range result.Providers {
		if current[provider.Provider] == "" || result.CredentialFingerprints[provider.Provider] != current[provider.Provider] {
			revoked[provider.Provider] = true
		}
	}
	providers := result.Providers[:0]
	for _, p := range result.Providers {
		if !revoked[p.Provider] {
			providers = append(providers, p)
		}
	}
	result.Providers = providers
	writes := result.CacheWrites[:0]
	for _, entry := range result.CacheWrites {
		if !revoked[entry.SourceID] {
			writes = append(writes, entry)
		}
	}
	result.CacheWrites = writes
	found := map[string]bool{}
	for i := range result.SourceOutcomes {
		o := &result.SourceOutcomes[i]
		if revoked[o.SourceID] {
			o.Status = "failure"
			o.ErrorCode = "settings_changed"
			found[o.SourceID] = true
		}
	}
	for source := range revoked {
		if !found[source] {
			result.SourceOutcomes = append(result.SourceOutcomes, ipquality.SourceOutcome{SourceID: source, Status: "failure", ErrorCode: "settings_changed", ObservedAt: time.Now().UTC()})
		}
	}
	// Restore the exact public RIS/RDAP snapshot, not selected overlapping fields
	// from an authenticated response whose credential has since been revoked.
	if revoked["ipapi.is"] {
		result.ASN = agentapi.IPQualityASN{Status: "unknown", Origins: []string{}}
		if result.PublicASN != nil {
			result.ASN = *result.PublicASN
		}
	}
}

func intelligenceTerminalState(result ipquality.IntelligenceSnapshot) string {
	if result.State == "skipped" {
		return "skipped"
	}
	successes, failures := 0, 0
	for _, outcome := range result.SourceOutcomes {
		switch outcome.Status {
		case "success":
			successes++
		case "failure":
			failures++
		}
	}
	if successes > 0 && failures > 0 {
		return "partial"
	}
	if successes > 0 {
		return "complete"
	}
	return "failed"
}

func (r *PostgresIPQualityRepository) LoadIPQualitySourceCache(ctx context.Context, source, resource, fingerprint string) (*ipquality.SourceCacheEntry, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("invalid intelligence repository")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if source == "ipapi.is" || source == "abuseipdb" || source == "maxmind" {
		settings, err := lockIPQualityIntelligenceSettings(ctx, tx)
		if err != nil {
			return nil, err
		}
		current := intelligenceCredentials(settings)[source]
		if current == "" || current != fingerprint {
			return nil, nil
		}
	} else if fingerprint != "" {
		return nil, nil
	}
	var entry ipquality.SourceCacheEntry
	err = tx.QueryRow(ctx, `select source_id,resource,credential_fingerprint,fetched_at,expires_at,blocked_until,payload_json from ip_quality_source_cache
 where source_id=$1 and resource=$2 and credential_fingerprint=$3 and (expires_at>clock_timestamp() or (resource='@quota' and blocked_until>clock_timestamp()))`, source, resource, fingerprint).Scan(&entry.SourceID, &entry.Resource, &entry.CredentialFingerprint, &entry.FetchedAt, &entry.ExpiresAt, &entry.BlockedUntil, &entry.PayloadJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func validateIPQualityIntelligenceCache(entry ipquality.SourceCacheEntry, result ipquality.IntelligenceSnapshot, settings centersettings.IPQualitySettings) error {
	ttl := time.Duration(0)
	switch entry.SourceID {
	case "ipapi.is", "abuseipdb", "maxmind", "spamhaus-drop":
		ttl = time.Hour
	case "ripe-ris":
		ttl = 8 * time.Hour
	case "rdap-ip", "rdap-asn":
		ttl = 24 * time.Hour
	default:
		return fmt.Errorf("invalid intelligence cache source")
	}
	if entry.Resource == "@quota" {
		ttl = 24 * time.Hour
	}
	if entry.Resource == "" || len(entry.Resource) > 256 || entry.FetchedAt.IsZero() || !entry.ExpiresAt.After(entry.FetchedAt) || entry.ExpiresAt.Sub(entry.FetchedAt) > ttl {
		return fmt.Errorf("invalid intelligence cache lifetime")
	}
	credentials := intelligenceCredentials(settings)
	if entry.SourceID == "ipapi.is" || entry.SourceID == "abuseipdb" || entry.SourceID == "maxmind" {
		if credentials[entry.SourceID] == "" || entry.CredentialFingerprint != credentials[entry.SourceID] || result.CredentialFingerprints[entry.SourceID] != entry.CredentialFingerprint {
			return fmt.Errorf("invalid intelligence cache credential scope")
		}
	} else if entry.CredentialFingerprint != "" {
		return fmt.Errorf("invalid public cache credential scope")
	}
	if entry.Resource == "@quota" {
		if entry.BlockedUntil == nil || entry.BlockedUntil.Sub(entry.FetchedAt) < time.Minute || entry.BlockedUntil.Sub(entry.FetchedAt) > 24*time.Hour || string(entry.PayloadJSON) != "{}" {
			return fmt.Errorf("invalid intelligence quota cache")
		}
		return nil
	}
	if entry.BlockedUntil != nil || len(entry.PayloadJSON) == 0 || len(entry.PayloadJSON) > 8<<20 || !json.Valid(entry.PayloadJSON) {
		return fmt.Errorf("invalid intelligence cache payload")
	}
	for _, secret := range []string{settings.Sources.IPAPIISKey, settings.Sources.AbuseIPDBKey, settings.Sources.MaxMindLicenseKey} {
		if secret != "" && strings.Contains(string(entry.PayloadJSON), secret) {
			return fmt.Errorf("unsafe intelligence cache payload")
		}
	}
	if err := ipquality.ValidateIntelligenceCacheEntry(entry); err != nil {
		return fmt.Errorf("invalid intelligence cache data")
	}
	return nil
}
