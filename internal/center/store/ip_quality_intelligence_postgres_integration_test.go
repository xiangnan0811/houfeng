package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationIPQualityIntelligenceOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-intelligence-owner", 6)
	repo := NewPostgresIPQualityRepository(pool)
	settingsRepo := NewPostgresSettingsRepository(pool, centersettings.DeploymentDefaults{})
	_, err := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		s.IPQuality.Enabled = true
		return s, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	newJob := func(prefix string) string {
		t.Helper()
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		now := time.Now().UTC()
		report := ipQualityProductIntegrationReport(prefix, now.Add(-time.Second), now, false)
		if err := repo.SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
			t.Fatal(err)
		}
		return readIPQualityProductReportID(t, ctx, fixture, "mi_"+prefix, report.SyncBatchID)
	}
	claimJob := func(id, owner string) *ipquality.IntelligenceClaim {
		t.Helper()
		claim, err := repo.ClaimIPQualityIntelligence(ctx, owner)
		if err != nil || claim == nil || claim.ReportID != id {
			t.Fatalf("claim %s: %#v %v", id, claim, err)
		}
		return claim
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() ipquality.IntelligenceSnapshot {
		return ipquality.IntelligenceSnapshot{State: "complete", Providers: []ipquality.ProviderResultRead{}, Blacklists: []agentapi.IPQualityBlacklistResult{}, Registration: agentapi.IPQualityRegistration{Status: "unknown"}, ASN: agentapi.IPQualityASN{Status: "unknown", Origins: []string{}}, SourceOutcomes: []ipquality.SourceOutcome{{SourceID: "ripe-ris", Status: "success", ObservedAt: time.Now().UTC()}}}
	}
	terminal := func(id, want string) {
		t.Helper()
		var state string
		var completed bool
		if err := pool.QueryRow(ctx, `select state,completed_at is not null from ip_quality_intelligence where report_id=$1`, id).Scan(&state, &completed); err != nil {
			t.Fatal(err)
		}
		if state != want || !completed {
			t.Fatalf("terminal %s = %s/%t want %s/true", id, state, completed, want)
		}
	}

	t.Run("two workers and immutable terminal", func(t *testing.T) {
		id := newJob("ipqi_two")
		type result struct {
			claim *ipquality.IntelligenceClaim
			err   error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		for _, owner := range []string{"worker-a", "worker-b"} {
			go func(owner string) {
				<-start
				c, e := repo.ClaimIPQualityIntelligence(ctx, owner)
				results <- result{c, e}
			}(owner)
		}
		close(start)
		var claim *ipquality.IntelligenceClaim
		for range 2 {
			got := waitForRecordPlatformResult(t, results, "claim")
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.claim != nil {
				if claim != nil {
					t.Fatal("two workers owned one job")
				}
				claim = got.claim
			}
		}
		if claim == nil || claim.ReportID != id || claim.AttemptCount != 1 || claim.LeaseUntil.After(claim.DeadlineAt) || time.Until(claim.LeaseUntil) > 120*time.Second {
			t.Fatalf("claim = %#v", claim)
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, snapshot()); err != nil {
			t.Fatal(err)
		}
		terminal(id, "complete")
		var before string
		if err := pool.QueryRow(ctx, `select data_json::text || summary_json::text from ip_quality_intelligence where report_id=$1`, id).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, snapshot()); !errors.Is(err, ipquality.ErrIntelligenceOwnership) {
			t.Fatalf("terminal overwrite = %v", err)
		}
		if again, err := repo.ClaimIPQualityIntelligence(ctx, "worker-c"); err != nil || again != nil {
			t.Fatalf("terminal reopened: %#v %v", again, err)
		}
		var after string
		if err := pool.QueryRow(ctx, `select data_json::text || summary_json::text from ip_quality_intelligence where report_id=$1`, id).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatal("terminal snapshot changed")
		}
	})
	t.Run("lease recovery and live third attempt", func(t *testing.T) {
		id := newJob("ipqi_third")
		first := claimJob(id, "crashed-first")
		exec(`update ip_quality_intelligence set lease_until=clock_timestamp()-interval '1 second' where report_id=$1`, id)
		second := claimJob(id, "recovered-second")
		if second.AttemptCount != 2 || second.OwnerGeneration <= first.OwnerGeneration {
			t.Fatal("lease recovery did not fence first owner")
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *first, snapshot()); !errors.Is(err, ipquality.ErrIntelligenceOwnership) {
			t.Fatalf("old owner write=%v", err)
		}
		exec(`update ip_quality_intelligence set lease_until=clock_timestamp()-interval '1 second' where report_id=$1`, id)
		third := claimJob(id, "third-live")
		if third.AttemptCount != 3 {
			t.Fatal("third claim absent")
		}
		if got, err := repo.ClaimIPQualityIntelligence(ctx, "sweeper"); err != nil || got != nil {
			t.Fatalf("live third claim stolen %#v %v", got, err)
		}
		if ok, err := repo.IPQualityIntelligenceEligible(ctx, *third); err != nil || !ok {
			t.Fatalf("live third claim swept: %t %v", ok, err)
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *third, snapshot()); err != nil {
			t.Fatal(err)
		}
		terminal(id, "complete")
	})
	t.Run("expired third lease and deadline reject late writes including cache", func(t *testing.T) {
		for _, reason := range []string{"third", "deadline"} {
			id := newJob("ipqi_expired_" + reason)
			claim := claimJob(id, "expired-owner")
			if reason == "third" {
				exec(`update ip_quality_intelligence set attempt_count=3,lease_until=clock_timestamp()-interval '1 second' where report_id=$1`, id)
			} else {
				exec(`update ip_quality_intelligence set deadline_at=clock_timestamp()-interval '1 second',lease_until=clock_timestamp()+interval '90 seconds' where report_id=$1`, id)
			}
			result := snapshot()
			now := time.Now().UTC()
			result.CacheWrites = []ipquality.SourceCacheEntry{{SourceID: "ripe-ris", Resource: claim.Report.IPAddress, PayloadJSON: json.RawMessage(`{"status":"unknown","origins":[]}`), FetchedAt: now, ExpiresAt: now.Add(time.Hour)}}
			if err := repo.CompleteIPQualityIntelligence(ctx, *claim, result); !errors.Is(err, ipquality.ErrIntelligenceOwnership) {
				t.Fatalf("%s late write=%v", reason, err)
			}
			if got, err := repo.ClaimIPQualityIntelligence(ctx, "sweeper"); err != nil || got != nil {
				t.Fatalf("expired claimed %#v %v", got, err)
			}
			terminal(id, "failed")
			if err := repo.CompleteIPQualityIntelligence(ctx, *claim, result); !errors.Is(err, ipquality.ErrIntelligenceOwnership) {
				t.Fatalf("swept %s owner submitted: %v", reason, err)
			}
			var count int
			if err := pool.QueryRow(ctx, `select count(*)::int from ip_quality_source_cache where resource=$1`, claim.Report.IPAddress).Scan(&count); err != nil || count != 0 {
				t.Fatalf("expired cache saved: %d %v", count, err)
			}
		}
	})
	t.Run("near deadline bounds lease and pending TTL is terminal", func(t *testing.T) {
		id := newJob("ipqi_near")
		exec(`update ip_quality_intelligence set deadline_at=clock_timestamp()+interval '10 seconds' where report_id=$1`, id)
		claim := claimJob(id, "near-deadline")
		if !claim.LeaseUntil.Equal(claim.DeadlineAt) {
			t.Fatal("near-deadline lease extended beyond job deadline")
		}
		exec(`update ip_quality_intelligence set deadline_at=clock_timestamp()-interval '1 second' where report_id=$1`, id)
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, snapshot()); !errors.Is(err, ipquality.ErrIntelligenceOwnership) {
			t.Fatalf("near-deadline late result accepted: %v", err)
		}
		if c, err := repo.ClaimIPQualityIntelligence(ctx, "ttl-sweeper"); err != nil || c != nil {
			t.Fatalf("deadline recovery %#v %v", c, err)
		}
		terminal(id, "failed")
		pending := newJob("ipqi_pending_ttl")
		exec(`update ip_quality_intelligence set deadline_at=clock_timestamp()-interval '1 second' where report_id=$1`, pending)
		if c, err := repo.ClaimIPQualityIntelligence(ctx, "pending-sweeper"); err != nil || c != nil {
			t.Fatalf("pending TTL claim %#v %v", c, err)
		}
		terminal(pending, "failed")
	})
	t.Run("lifecycle checks before request and finalize", func(t *testing.T) {
		for i, mutation := range []string{"paused", "fingerprint", "archived", "retired", "link", "instance_archived", "disabled"} {
			prefix := fmt.Sprintf("ipqi_life_%d", i)
			id := newJob(prefix)
			claim := claimJob(id, "lifecycle-owner")
			switch mutation {
			case "paused":
				exec(`update monitoring_instances set monitoring_status=$2 where monitoring_instance_id=$1`, "mi_"+prefix, monitoringinstances.MonitoringPaused)
			case "fingerprint":
				exec(`update monitoring_instances set binding_fingerprint='new-binding' where monitoring_instance_id=$1`, "mi_"+prefix)
			case "archived":
				exec(`update vps_assets set lifecycle_status='archived' where vps_id=$1`, "vps_"+prefix)
			case "retired":
				exec(`update monitoring_instances set lifecycle_status=$2 where monitoring_instance_id=$1`, "mi_"+prefix, monitoringinstances.LifecycleRetired)
			case "link":
				exec(`update vps_monitoring_instance_links set unlinked_at=clock_timestamp() where monitoring_instance_id=$1`, "mi_"+prefix)
			case "instance_archived":
				exec(`update monitoring_instances set archived_at=clock_timestamp() where monitoring_instance_id=$1`, "mi_"+prefix)
			case "disabled":
				_, err = settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
					s.IPQuality.Enabled = false
					return s, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if ok, err := repo.IPQualityIntelligenceEligible(ctx, *claim); err != nil || ok {
				t.Fatalf("%s remains eligible: %t %v", mutation, ok, err)
			}
			if err := repo.CompleteIPQualityIntelligence(ctx, *claim, snapshot()); err != nil {
				t.Fatal(err)
			}
			terminal(id, "skipped")
			if mutation == "disabled" {
				_, err = settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
					s.IPQuality.Enabled = true
					return s, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		id := newJob("ipqi_preclaim")
		exec(`update monitoring_instances set monitoring_status=$2 where monitoring_instance_id=$1`, "mi_ipqi_preclaim", monitoringinstances.MonitoringPaused)
		if c, err := repo.ClaimIPQualityIntelligence(ctx, "paused-claim"); err != nil || c != nil {
			t.Fatalf("ineligible job claimed %#v %v", c, err)
		}
		terminal(id, "skipped")
	})
	t.Run("credential revocation atomic in both commit orders", func(t *testing.T) {
		for _, revokeFirst := range []bool{true, false} {
			prefix := fmt.Sprintf("ipqi_revoke_%t", revokeFirst)
			id := newJob(prefix)
			key := "fixture-ipapi-secret-" + prefix
			_, err := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				s.IPQuality.Sources.IPAPIISKey = key
				return s, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			claim := claimJob(id, "credential-owner")
			result := snapshot()
			details := *claim.Report.ProviderResults[0].Details
			details.Method = "center_api"
			details.APITier = "authenticated"
			details.ObservedAt = time.Now().UTC()
			facts := claim.Report.ProviderResults[0]
			provider := ipquality.ProviderResultRead{Provider: "ipapi.is", Status: "success", Details: &details}
			// Only carry booleans which this typed fixture explicitly marks known;
			// legacy carrier fields without typed evidence are not authenticated facts.
			for field, value := range map[string]*bool{"is_proxy": facts.IsProxy, "is_vpn": facts.IsVPN, "is_tor": facts.IsTor, "is_server": facts.IsServer, "is_abuser": facts.IsAbuser, "is_robot": facts.IsRobot} {
				if details.FieldStates[field] != "known" {
					continue
				}
				switch field {
				case "is_proxy":
					provider.IsProxy = value
				case "is_vpn":
					provider.IsVPN = value
				case "is_tor":
					provider.IsTor = value
				case "is_server":
					provider.IsServer = value
				case "is_abuser":
					provider.IsAbuser = value
				case "is_robot":
					provider.IsRobot = value
				}
			}
			result.Providers = []ipquality.ProviderResultRead{provider}
			result.CredentialFingerprints = map[string]string{"ipapi.is": intelligenceFingerprint(key)}
			public := agentapi.IPQualityASN{Status: "success", Number: "AS64500", Name: "public holder", Origins: []string{"AS64500"}}
			result.PublicASN = &public
			result.ASN = public
			result.ASN.Name = "authenticated holder"
			result.ASN.Domain = "authenticated.example"
			result.ASN.RIR = "authenticated RIR"
			result.SourceOutcomes = append(result.SourceOutcomes, ipquality.SourceOutcome{SourceID: "ipapi.is", Status: "success", ObservedAt: time.Now().UTC()})
			data, _ := json.Marshal(provider)
			now := time.Now().UTC()
			result.CacheWrites = []ipquality.SourceCacheEntry{{SourceID: "ipapi.is", Resource: claim.Report.IPAddress, CredentialFingerprint: intelligenceFingerprint(key), FetchedAt: now, ExpiresAt: now.Add(time.Hour), PayloadJSON: data}}
			clear := func() (centersettings.CenterSettings, error) {
				return settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
					s.IPQuality.Sources.IPAPIISKey = ""
					return s, nil
				})
			}
			if revokeFirst {
				entered, release := make(chan struct{}), make(chan struct{})
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				cleared := make(chan error, 1)
				go func() {
					_, e := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
						close(entered)
						<-release
						s.IPQuality.Sources.IPAPIISKey = ""
						return s, nil
					})
					cleared <- e
				}()
				<-entered
				finalized := make(chan error, 1)
				go func() { finalized <- repo.CompleteIPQualityIntelligence(ctx, *claim, result) }()
				waitForRecordsPostgresLockWaiter(t, ctx, fixture.db, "%center_settings%")
				close(release)
				if err := waitForRecordPlatformResult(t, cleared, "clear credential"); err != nil {
					t.Fatal(err)
				}
				if err := waitForRecordPlatformResult(t, finalized, "finalize revoked query"); err != nil {
					t.Fatal(err)
				}
			} else {
				// Hold the report row so finalize has already locked settings FOR SHARE
				// before clear attempts its settings FOR UPDATE.
				blocker, err := fixture.db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = blocker.Rollback(ctx) }()
				if _, err = blocker.Exec(ctx, `select report_id from ip_quality_reports where report_id=$1 for update`, id); err != nil {
					t.Fatal(err)
				}
				finalized := make(chan error, 1)
				go func() { finalized <- repo.CompleteIPQualityIntelligence(ctx, *claim, result) }()
				waitForRecordsPostgresLockWaiter(t, ctx, fixture.db, "%ip_quality_reports%for share%")
				cleared := make(chan error, 1)
				go func() { _, err := clear(); cleared <- err }()
				waitForRecordsPostgresLockWaiter(t, ctx, fixture.db, "%center_settings%for update%")
				if err = blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if err := waitForRecordPlatformResult(t, finalized, "finalize before revoke"); err != nil {
					t.Fatal(err)
				}
				if err := waitForRecordPlatformResult(t, cleared, "clear after finalize"); err != nil {
					t.Fatal(err)
				}
			}
			var raw []byte
			var state string
			if err := pool.QueryRow(ctx, `select data_json,state from ip_quality_intelligence where report_id=$1`, id).Scan(&raw, &state); err != nil {
				t.Fatal(err)
			}
			var stored ipquality.IntelligenceSnapshot
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatal(err)
			}
			var caches int
			if err := pool.QueryRow(ctx, `select count(*)::int from ip_quality_source_cache where source_id='ipapi.is' and credential_fingerprint=$1`, intelligenceFingerprint(key)).Scan(&caches); err != nil {
				t.Fatal(err)
			}
			if revokeFirst {
				if len(stored.Providers) != 0 || !reflect.DeepEqual(stored.ASN, public) || state != "partial" || caches != 0 {
					t.Fatalf("revoked facts/cache retained: %#v state=%s cache=%d", stored, state, caches)
				}
				found := false
				for _, o := range stored.SourceOutcomes {
					if o.SourceID == "ipapi.is" && o.ErrorCode == "settings_changed" {
						found = true
					}
				}
				if !found {
					t.Fatal("missing revocation outcome")
				}
			} else {
				if len(stored.Providers) != 1 || stored.ASN.Name != "authenticated holder" || state != "complete" || caches != 1 {
					t.Fatalf("legally completed history discarded: %#v %s %d", stored, state, caches)
				}
			}
			if cached, err := repo.LoadIPQualitySourceCache(ctx, "ipapi.is", claim.Report.IPAddress, intelligenceFingerprint(key)); err != nil || cached != nil {
				t.Fatalf("cleared credential loaded old cache %#v %v", cached, err)
			}
		}
	})
	t.Run("cache expiration cleanup quota and snapshot validation", func(t *testing.T) {
		id := newJob("ipqi_cache")
		claim := claimJob(id, "cache-owner")
		result := snapshot()
		now := time.Now().UTC()
		until := now.Add(24 * time.Hour)
		cachedASN, err := json.Marshal(agentapi.IPQualityASN{Status: "success", Number: "AS64500", Name: "Fixture network", Origins: []string{"AS64500"}, Route: "203.0.113.0/24", ObservedAt: &now})
		if err != nil {
			t.Fatal(err)
		}
		result.CacheWrites = []ipquality.SourceCacheEntry{{SourceID: "ripe-ris", Resource: claim.Report.IPAddress, FetchedAt: now, ExpiresAt: now.Add(8 * time.Hour), PayloadJSON: cachedASN}, {SourceID: "ripe-ris", Resource: "@quota", FetchedAt: now, ExpiresAt: until, BlockedUntil: &until, PayloadJSON: json.RawMessage(`{}`)}}
		bad := result
		bad.Providers = []ipquality.ProviderResultRead{{Provider: "unknown", Status: "success"}}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, bad); err == nil {
			t.Fatal("invalid typed snapshot accepted")
		}
		invalidCache := snapshot()
		invalidCache.CacheWrites = []ipquality.SourceCacheEntry{result.CacheWrites[0]}
		invalidCache.CacheWrites[0].PayloadJSON = json.RawMessage(`{"status":"unknown","origins":[]}`)
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, invalidCache); err == nil {
			t.Fatal("unknown ASN facts accepted as successful cache data")
		}
		oversized := snapshot()
		oversized.Blacklists = []agentapi.IPQualityBlacklistResult{{Source: "spamcop", Status: "unknown", QueryMethod: "dnsbl", ObservedAt: now, Codes: []string{strings.Repeat("x", 513<<10)}}}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, oversized); err == nil {
			t.Fatal("snapshot over 512KiB accepted")
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, result); err != nil {
			t.Fatal(err)
		}
		cache, err := repo.LoadIPQualitySourceCache(ctx, "ripe-ris", claim.Report.IPAddress, "")
		if err != nil || cache == nil {
			t.Fatalf("cache miss %#v %v", cache, err)
		}
		exec(`update ip_quality_source_cache set expires_at=clock_timestamp()-interval '25 hours' where source_id='ripe-ris'`)
		if cache, err = repo.LoadIPQualitySourceCache(ctx, "ripe-ris", claim.Report.IPAddress, ""); err != nil || cache != nil {
			t.Fatal("expired success cache served")
		}
		if quota, err := repo.LoadIPQualitySourceCache(ctx, "ripe-ris", "@quota", ""); err != nil || quota == nil {
			t.Fatal("active quota forgotten")
		}
		if _, err = repo.ClaimIPQualityIntelligence(ctx, "cleanup"); err != nil {
			t.Fatal(err)
		}
		var successes, quotas int
		if err = pool.QueryRow(ctx, `select count(*) filter(where resource<>'@quota')::int,count(*) filter(where resource='@quota')::int from ip_quality_source_cache where source_id='ripe-ris'`).Scan(&successes, &quotas); err != nil || successes != 0 || quotas != 1 {
			t.Fatalf("cache cleanup=%d/%d %v", successes, quotas, err)
		}
	})
	t.Run("bounded discovery cleans expired backlog across passes", func(t *testing.T) {
		const prefix = "ipqi_backlog"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		reports := make([]ipquality.ReportWrite, 65)
		now := time.Now().UTC()
		for i := range reports {
			reports[i] = ipQualityProductIntegrationReport(prefix, now.Add(time.Duration(i)*time.Second), now, false)
			reports[i].SyncBatchID = fmt.Sprintf("sync_backlog_%d", i)
		}
		if err := repo.SaveReports(ctx, reports); err != nil {
			t.Fatal(err)
		}
		exec(`update ip_quality_intelligence set deadline_at=clock_timestamp()-interval '1 second'
   where report_id in (select report_id from ip_quality_reports where monitoring_instance_id=$1)`, "mi_"+prefix)
		for pass, wantFailed := range []int{64, 65} {
			if claim, err := repo.ClaimIPQualityIntelligence(ctx, "backlog-sweeper"); err != nil || claim != nil {
				t.Fatalf("backlog claim=%#v %v", claim, err)
			}
			var failed, pending int
			if err := pool.QueryRow(ctx, `select count(*) filter(where i.state='failed')::int,count(*) filter(where i.state='pending')::int
    from ip_quality_intelligence i join ip_quality_reports r on r.report_id=i.report_id
    where r.monitoring_instance_id=$1`, "mi_"+prefix).Scan(&failed, &pending); err != nil {
				t.Fatal(err)
			}
			if failed != wantFailed || pending != 65-wantFailed {
				t.Fatalf("pass %d cleanup=%d/%d, want %d/%d", pass, failed, pending, wantFailed, 65-wantFailed)
			}
		}
	})
	t.Run("eligibility read error then recovers preserves facts", func(t *testing.T) {
		id := newJob("ipqi_elig")
		claim := claimJob(id, "eligibility-owner")
		failedCtx, cancelRead := context.WithCancel(ctx)
		cancelRead()
		if _, err := repo.IPQualityIntelligenceEligible(failedCtx, *claim); err == nil {
			t.Fatal("eligibility read error reported as a clean revocation")
		}
		if ok, err := repo.IPQualityIntelligenceEligible(ctx, *claim); err != nil || !ok {
			t.Fatalf("eligibility did not recover: %t %v", ok, err)
		}
		now := time.Now().UTC()
		asn := agentapi.IPQualityASN{Status: "success", Number: "AS64500", Name: "Recovered network", Origins: []string{"AS64500"}, Route: claim.Report.IPAddress + "/32", ObservedAt: &now}
		cached, err := json.Marshal(asn)
		if err != nil {
			t.Fatal(err)
		}
		// Collector handoff after an eligibility read error: further requests stop,
		// collected facts and cache stay, and the terminal state comes from outcomes.
		result := ipquality.IntelligenceSnapshot{
			State:        "partial",
			Providers:    []ipquality.ProviderResultRead{},
			Blacklists:   []agentapi.IPQualityBlacklistResult{{Source: "spamcop", Category: "spam", Status: "unknown", QueryMethod: "dnsbl", ErrorCode: "eligibility_failed", Codes: []string{}, ObservedAt: now}},
			Registration: agentapi.IPQualityRegistration{Status: "unknown"},
			ASN:          asn,
			SourceOutcomes: []ipquality.SourceOutcome{
				{SourceID: "ripe-ris", Status: "success", ObservedAt: now},
				{SourceID: "spamcop", Status: "failure", ErrorCode: "eligibility_failed", ObservedAt: now},
			},
			CacheWrites: []ipquality.SourceCacheEntry{{SourceID: "ripe-ris", Resource: claim.Report.IPAddress, FetchedAt: now, ExpiresAt: now.Add(time.Hour), PayloadJSON: cached}},
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, result); err != nil {
			t.Fatal(err)
		}
		var state, errorCode, data string
		if err := pool.QueryRow(ctx, `select state,error_code,data_json::text from ip_quality_intelligence where report_id=$1`, id).Scan(&state, &errorCode, &data); err != nil {
			t.Fatal(err)
		}
		if state != "partial" || errorCode != "" || !strings.Contains(data, "AS64500") || !strings.Contains(data, "eligibility_failed") {
			t.Fatalf("recovered finalize = %s/%s/%s, want partial facts and no lifecycle skip", state, errorCode, data)
		}
		if cache, err := repo.LoadIPQualitySourceCache(ctx, "ripe-ris", claim.Report.IPAddress, ""); err != nil || cache == nil || !strings.Contains(string(cache.PayloadJSON), "AS64500") {
			t.Fatalf("recovered cache = %#v %v, want preserved public fact", cache, err)
		}
		page, err := repo.GetVPSIPQuality(ctx, "vps_ipqi_elig", 4)
		if err != nil || page.Product == nil || page.Product.State != "partial" || page.Product.ASN.Number != "AS64500" || len(page.Product.Blacklists) != 1 || page.Product.Blacklists[0].Status != "unknown" {
			t.Fatalf("recovered page = %#v %v, want preserved ASN and blacklist diagnostic", page.Product, err)
		}
		if page.Summary == nil || page.Summary.ProductSummary == nil || len(page.Summary.ProductSummary.RiskFlags) == 0 || page.Summary.ProductSummary.RiskFlags[0] != "vpn" {
			t.Fatalf("recovered summary = %#v, want agent VPN fact kept", page.Summary)
		}

		// A collector skipped flag is not revocation if the locked recheck is eligible.
		falseSkipID := newJob("ipqi_elig_skip")
		falseSkip := claimJob(falseSkipID, "false-skip-owner")
		falseSkipResult := result
		falseSkipResult.State = "skipped"
		falseSkipResult.CacheWrites = nil
		falseSkipResult.ASN.Route = falseSkip.Report.IPAddress + "/32"
		if err := repo.CompleteIPQualityIntelligence(ctx, *falseSkip, falseSkipResult); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select state,error_code,data_json::text from ip_quality_intelligence where report_id=$1`, falseSkipID).Scan(&state, &errorCode, &data); err != nil {
			t.Fatal(err)
		}
		if state != "partial" || errorCode != "" || !strings.Contains(data, "AS64500") || !strings.Contains(data, "eligibility_failed") {
			t.Fatalf("false skip finalize = %s/%s/%s, want partial facts and no lifecycle skip", state, errorCode, data)
		}

		revokedID := newJob("ipqi_elig_revoke")
		revoked := claimJob(revokedID, "eligibility-revoke")
		exec(`update monitoring_instances set monitoring_status=$2 where monitoring_instance_id=$1`, "mi_ipqi_elig_revoke", monitoringinstances.MonitoringPaused)
		exec(`delete from ip_quality_source_cache where source_id='ripe-ris' and resource=$1`, revoked.Report.IPAddress)
		revokedResult := result
		revokedResult.State = "complete"
		revokedResult.CacheWrites = []ipquality.SourceCacheEntry{{SourceID: "ripe-ris", Resource: revoked.Report.IPAddress, FetchedAt: now, ExpiresAt: now.Add(time.Hour), PayloadJSON: cached}}
		revokedResult.ASN.Route = revoked.Report.IPAddress + "/32"
		if err := repo.CompleteIPQualityIntelligence(ctx, *revoked, revokedResult); err != nil {
			t.Fatal(err)
		}
		var revokedData string
		if err := pool.QueryRow(ctx, `select state,error_code,data_json::text from ip_quality_intelligence where report_id=$1`, revokedID).Scan(&state, &errorCode, &revokedData); err != nil {
			t.Fatal(err)
		}
		if state != "skipped" || errorCode != "lifecycle_changed" || strings.Contains(revokedData, "AS64500") || strings.Contains(revokedData, "eligibility_failed") {
			t.Fatalf("authoritative revoke = %s/%s/%s, want cleared skip", state, errorCode, revokedData)
		}
		if cache, err := repo.LoadIPQualitySourceCache(ctx, "ripe-ris", revoked.Report.IPAddress, ""); err != nil || cache != nil {
			t.Fatalf("revoked cache = %#v %v, want no write", cache, err)
		}
	})
	t.Run("failed finalize exposes diagnostics and keeps agent summary", func(t *testing.T) {
		id := newJob("ipqi_failget")
		claim := claimJob(id, "failed-owner")
		now := time.Now().UTC()
		key := "fixture-abuseipdb-secret-ipqi-failget"
		if _, err := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			s.IPQuality.Sources.AbuseIPDBKey = key
			return s, nil
		}); err != nil {
			t.Fatal(err)
		}
		result := ipquality.IntelligenceSnapshot{
			State: "failed",
			Providers: []ipquality.ProviderResultRead{{
				Provider: "abuseipdb", Status: "failure", SourceType: "default", ErrorCode: "http_failed",
				Details: &agentapi.IPQualityProviderDetails{
					QueriedIP: claim.Report.IPAddress, ObservedAt: now, Method: "center_api", APITier: "authenticated",
					Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{},
				},
			}},
			Blacklists:             []agentapi.IPQualityBlacklistResult{{Source: "spamcop", Category: "spam", Status: "unknown", QueryMethod: "dnsbl", ErrorCode: "dns_failed", Codes: []string{}, ObservedAt: now}},
			Registration:           agentapi.IPQualityRegistration{Status: "unknown"},
			ASN:                    agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
			CredentialFingerprints: map[string]string{"abuseipdb": intelligenceFingerprint(key)},
			SourceOutcomes: []ipquality.SourceOutcome{
				{SourceID: "abuseipdb", Status: "failure", ErrorCode: "http_failed", ObservedAt: now},
				{SourceID: "spamcop", Status: "failure", ErrorCode: "dns_failed", ObservedAt: now},
			},
		}
		if err := repo.CompleteIPQualityIntelligence(ctx, *claim, result); err != nil {
			t.Fatal(err)
		}
		terminal(id, "failed")
		var agentRaw []byte
		if err := pool.QueryRow(ctx, `select product_summary_json from ip_quality_reports where report_id=$1`, id).Scan(&agentRaw); err != nil {
			t.Fatal(err)
		}
		var agentSummary agentapi.IPQualityProductSummary
		if err := json.Unmarshal(agentRaw, &agentSummary); err != nil {
			t.Fatal(err)
		}
		agentSummary = normalizeIPQualityProductSummary(agentSummary)
		assertFailedPage := func(page ipquality.VPSReport) {
			t.Helper()
			if page.Product == nil || page.Product.State != "failed" || page.Summary == nil || page.Summary.IntelligenceState != "failed" || page.Summary.ProductSummary == nil || !reflect.DeepEqual(*page.Summary.ProductSummary, agentSummary) {
				t.Fatalf("failed summary = %#v product=%#v, want stored agent summary", page.Summary, page.Product)
			}
			foundFailure, foundVPN, foundUnknown := false, false, false
			for _, row := range page.Product.Providers {
				if row.Provider == "abuseipdb" && row.Status == "failure" && row.ErrorCode == "http_failed" && row.IsVPN == nil && len(row.ExtraJSON) == 0 && row.Details != nil && row.Details.Method == "center_api" && row.Details.APITier == "authenticated" && row.Details.City == "" {
					foundFailure = true
				}
				if row.Provider == "ipapi.is" && row.IsVPN != nil && *row.IsVPN {
					foundVPN = true
				}
			}
			for _, row := range page.Product.Blacklists {
				if row.Source == "spamcop" && row.Status == "unknown" && row.ErrorCode == "dns_failed" {
					foundUnknown = true
				}
			}
			if !foundFailure || !foundVPN || !foundUnknown || len(page.ProviderResults) != 1 || page.ProviderResults[0].IsVPN == nil || !*page.ProviderResults[0].IsVPN {
				t.Fatalf("failed diagnostics = product:%#v providers:%#v", page.Product, page.ProviderResults)
			}
			if len(page.History) == 0 || page.History[0].IntelligenceState != "failed" || page.History[0].ProductSummary == nil || !reflect.DeepEqual(*page.History[0].ProductSummary, agentSummary) {
				t.Fatalf("failed history = %#v, want the same agent summary", page.History)
			}
		}
		page, err := repo.GetVPSIPQuality(ctx, "vps_ipqi_failget", 4)
		if err != nil {
			t.Fatal(err)
		}
		assertFailedPage(page)
		overview, err := repo.GetLatestVPSIPQualitySummary(ctx, "vps_ipqi_failget")
		if err != nil || overview == nil || overview.IntelligenceState != "failed" || overview.ProductSummary == nil || !reflect.DeepEqual(*overview.ProductSummary, agentSummary) {
			t.Fatalf("failed overview = %#v %v, want agent summary", overview, err)
		}
		poison, err := json.Marshal(agentapi.IPQualityProductSummary{RiskFlags: []string{"abuse"}, HighRiskSources: []string{"abuseipdb"}, BlacklistListedCount: 1, BlacklistCheckedCount: 1, GeoStatus: "consistent"})
		if err != nil {
			t.Fatal(err)
		}
		exec(`update ip_quality_intelligence set summary_json=$2::jsonb where report_id=$1`, id, poison)
		poisoned, err := repo.GetVPSIPQuality(ctx, "vps_ipqi_failget", 4)
		if err != nil {
			t.Fatal(err)
		}
		assertFailedPage(poisoned)
		poisonedOverview, err := repo.GetLatestVPSIPQualitySummary(ctx, "vps_ipqi_failget")
		if err != nil || poisonedOverview == nil || poisonedOverview.ProductSummary == nil || !reflect.DeepEqual(*poisonedOverview.ProductSummary, agentSummary) {
			t.Fatalf("poisoned overview = %#v %v, want agent summary", poisonedOverview, err)
		}

		sweptID := newJob("ipqi_swept")
		exec(`update ip_quality_intelligence set deadline_at=clock_timestamp()-interval '1 second' where report_id=$1`, sweptID)
		if claim, err := repo.ClaimIPQualityIntelligence(ctx, "swept-empty"); err != nil || claim != nil {
			t.Fatalf("swept claim = %#v %v", claim, err)
		}
		var sweptNull bool
		if err := pool.QueryRow(ctx, `select state='failed' and error_code='deadline_or_attempts_exhausted' and data_json is null from ip_quality_intelligence where report_id=$1`, sweptID).Scan(&sweptNull); err != nil || !sweptNull {
			t.Fatalf("swept row null=%t err=%v", sweptNull, err)
		}
		sweptPage, err := repo.GetVPSIPQuality(ctx, "vps_ipqi_swept", 4)
		if err != nil || sweptPage.Product == nil || sweptPage.Product.State != "failed" || len(sweptPage.Product.Blacklists) != 0 || len(sweptPage.Product.Providers) != 1 || sweptPage.Product.Providers[0].Provider != "ipapi.is" || sweptPage.Summary == nil || sweptPage.Summary.ProductSummary == nil || len(sweptPage.Summary.ProductSummary.RiskFlags) == 0 || sweptPage.Summary.ProductSummary.RiskFlags[0] != "vpn" {
			t.Fatalf("swept page = %#v %v, want no center diagnostics", sweptPage.Product, err)
		}

		skippedID := newJob("ipqi_skipempty")
		skippedClaim := claimJob(skippedID, "skip-empty")
		exec(`update monitoring_instances set monitoring_status=$2 where monitoring_instance_id=$1`, "mi_ipqi_skipempty", monitoringinstances.MonitoringPaused)
		skippedResult := result
		skippedResult.State = "complete"
		skippedResult.Blacklists = []agentapi.IPQualityBlacklistResult{{Source: "spamcop", Category: "spam", Status: "listed", QueryMethod: "dnsbl", Codes: []string{"127.0.0.2"}, ObservedAt: now}}
		skippedResult.Providers[0].Details.QueriedIP = skippedClaim.Report.IPAddress
		if err := repo.CompleteIPQualityIntelligence(ctx, *skippedClaim, skippedResult); err != nil {
			t.Fatal(err)
		}
		var skippedState, skippedCode, skippedData string
		if err := pool.QueryRow(ctx, `select state,error_code,data_json::text from ip_quality_intelligence where report_id=$1`, skippedID).Scan(&skippedState, &skippedCode, &skippedData); err != nil {
			t.Fatal(err)
		}
		if skippedState != "skipped" || skippedCode != "lifecycle_changed" || strings.Contains(skippedData, "listed") || strings.Contains(skippedData, "127.0.0.2") {
			t.Fatalf("skipped payload = %s/%s/%s, want cleared", skippedState, skippedCode, skippedData)
		}
		skippedPage, err := repo.GetVPSIPQuality(ctx, "vps_ipqi_skipempty", 4)
		if err != nil || skippedPage.Product == nil || skippedPage.Product.State != "skipped" || len(skippedPage.Product.Blacklists) != 0 || len(skippedPage.Product.Providers) != 1 || skippedPage.Summary == nil || skippedPage.Summary.ProductSummary == nil || len(skippedPage.Summary.ProductSummary.RiskFlags) == 0 || skippedPage.Summary.ProductSummary.RiskFlags[0] != "vpn" {
			t.Fatalf("skipped page = %#v %v, want empty center rows", skippedPage.Product, err)
		}
	})
}
