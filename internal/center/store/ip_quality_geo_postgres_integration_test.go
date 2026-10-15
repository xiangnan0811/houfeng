package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationIPQualityMaxMindPairSnapshotAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-maxmind", 4)
	repo := NewPostgresIPQualityRepository(pool)
	settingsRepo := NewPostgresSettingsRepository(pool, centersettings.DeploymentDefaults{})
	const account, key = "12345678", "maxmind-fixture-private-key"
	for _, mode := range []string{"unchanged", "account", "key", "clear"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				s.IPQuality.Enabled = true
				s.IPQuality.Sources.MaxMindAccountID, s.IPQuality.Sources.MaxMindLicenseKey = account, key
				return s, nil
			}); err != nil {
				t.Fatal(err)
			}
			prefix := "ipq_geo_" + mode
			seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
			now := time.Now().UTC()
			report := ipQualityProductIntegrationReport(prefix, now.Add(-time.Second), now, false)
			// Anonymous evidence deliberately differs from the optional official pair.
			report.ProviderResults = []ipquality.ProviderResultWrite{{Provider: "maxmind", Status: "success", SourceType: "default", Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: report.IPAddress, ObservedAt: report.ObservedAt, Method: "agent_public", APITier: "anonymous", CountryCode: "US", RegisteredCountryCode: "JP",
			}}}
			if err := repo.SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
				t.Fatal(err)
			}
			id := readIPQualityProductReportID(t, ctx, fixture, report.MonitoringInstanceID, report.SyncBatchID)
			claim, err := repo.ClaimIPQualityIntelligence(ctx, "maxmind-owner")
			if err != nil || claim == nil || claim.ReportID != id {
				t.Fatalf("claim=%+v err=%v", claim, err)
			}
			provider := ipquality.ProviderResultRead{Provider: "maxmind", Status: "success", SourceType: "api", Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: report.IPAddress, ObservedAt: now, Method: "center_api", APITier: "authenticated", CountryCode: "US", RegisteredCountryCode: "US", Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{"country_code": "known", "registered_country_code": "known"},
			}}
			data, _ := json.Marshal(provider)
			fp := centersettings.MaxMindCredentialFingerprint(account, key)
			result := ipquality.IntelligenceSnapshot{State: "complete", Providers: []ipquality.ProviderResultRead{provider},
				Registration: agentapi.IPQualityRegistration{Status: "unknown"}, ASN: agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
				CredentialFingerprints: map[string]string{"maxmind": fp}, SourceOutcomes: []ipquality.SourceOutcome{{SourceID: "maxmind", Status: "success", ObservedAt: now}},
				CacheWrites: []ipquality.SourceCacheEntry{{SourceID: "maxmind", Resource: report.IPAddress, CredentialFingerprint: fp, FetchedAt: now, ExpiresAt: now.Add(time.Hour), PayloadJSON: data}},
			}
			if mode != "unchanged" {
				if _, err := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
					switch mode {
					case "account":
						s.IPQuality.Sources.MaxMindAccountID = "87654321"
					case "key":
						s.IPQuality.Sources.MaxMindLicenseKey = "replacement-private-key"
					case "clear":
						s.IPQuality.Sources.MaxMindAccountID, s.IPQuality.Sources.MaxMindLicenseKey = "", ""
					}
					return s, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.CompleteIPQualityIntelligence(ctx, *claim, result); err != nil {
				t.Fatal(err)
			}
			var state string
			var raw, summary []byte
			if err := pool.QueryRow(ctx, `select state,data_json,summary_json from ip_quality_intelligence where report_id=$1`, id).Scan(&state, &raw, &summary); err != nil {
				t.Fatal(err)
			}
			var persisted agentapi.IPQualityProductSummary
			if err := json.Unmarshal(summary, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.IPType == nil {
				t.Fatal("finalization omitted IP type")
			}
			cache, err := repo.LoadIPQualitySourceCache(ctx, "maxmind", report.IPAddress, fp)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unchanged" {
				if state != "complete" || persisted.IPType.Status != "native" || cache == nil {
					t.Fatal("official same-source pair or cache was not finalized")
				}
				// Rotating only the account makes an already persisted cache inaccessible,
				// but must not rewrite this completed historical snapshot.
				if _, err := settingsRepo.MutateSettings(ctx, func(s centersettings.CenterSettings) (centersettings.CenterSettings, error) {
					s.IPQuality.Sources.MaxMindAccountID = "87654321"
					return s, nil
				}); err != nil {
					t.Fatal(err)
				}
				if old, err := repo.LoadIPQualitySourceCache(ctx, "maxmind", report.IPAddress, fp); err != nil || old != nil {
					t.Fatal("rotated account loaded old pair cache")
				}
				var after []byte
				if err := pool.QueryRow(ctx, `select summary_json from ip_quality_intelligence where report_id=$1`, id).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if string(after) != string(summary) {
					t.Fatal("settings rotation rewrote immutable history")
				}
			} else if state != "failed" || persisted.IPType.Status != "broadcast" || cache != nil || !strings.Contains(string(raw), "settings_changed") {
				t.Fatal("revoked official evidence replaced immutable anonymous pair or populated cache")
			}
			if strings.Contains(string(raw), key) || strings.Contains(string(raw), account) || strings.Contains(string(summary), key) {
				t.Fatal("credential persisted in report snapshot")
			}
		})
	}
}
