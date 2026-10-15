package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationIPQualityProductEvidenceBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	// Records source IDs require the production 16-character lowercase-hex
	// suffix; seed helpers derive both VPS and monitoring IDs from this prefix.
	const prefix = "0123456789abcde1"
	const recordID = "rec_ipqproductevidence"
	const snapshotID = "evs_ipqproductevidence"
	const credential = "ip-quality-evidence-fixture-key"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-product-evidence", 4)
	repository := NewPostgresIPQualityRepository(runtimePool)
	settingsRepository := NewPostgresSettingsRepository(runtimePool, centersettings.DeploymentDefaults{})
	if _, err := settingsRepository.MutateSettings(ctx, func(settings centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		settings.IPQuality.Enabled = true
		settings.IPQuality.Sources.IPAPIISKey = credential
		return settings, nil
	}); err != nil {
		t.Fatalf("enable intelligence: %v", err)
	}

	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	report := ipQualityProductIntegrationReport(prefix, now.Add(-2*time.Second), now.Add(-time.Second), false)
	report.Status = agentapi.IPQualityStatusPartial
	report.RiskLevel = ""
	report.ProviderResults[0].RiskLevel = ""
	report.ProviderResults[0].RiskScore = ""
	report.ProviderResults[0].Details.Scores = []agentapi.IPQualityScore{}
	// Every provider and service succeeded. Only SMTP timed out: that must not
	// revoke the successful provider's explicit VPN fact in Records v1.
	report.CoverageJSON = json.RawMessage(`{"expected_provider_count":1,"successful_provider_count":1,"expected_service_count":1,"successful_service_count":1}`)
	report.Product.Mail = []agentapi.IPQualityMailResult{{
		Provider: "gmail", Domain: "gmail.com", Status: "timeout",
		ErrorCode: "greeting_timeout", ObservedAt: report.ObservedAt,
	}}
	if err := repository.SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
		t.Fatalf("SaveReports(): %v", err)
	}
	reportID := readIPQualityProductReportID(t, ctx, fixture, report.MonitoringInstanceID, report.SyncBatchID)
	page, err := repository.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil {
		t.Fatalf("GetVPSIPQuality(pending): %v", err)
	}
	if page.Product == nil || page.Product.State != "pending" || len(page.Product.Mail) != 1 || page.Product.Mail[0].Status != "timeout" {
		t.Fatalf("pending product does not retain the SMTP failure: %#v", page.Product)
	}

	actor := storeEvidenceParticipantActor(t)
	selection := evidence.Selection{
		Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS),
		SourceID:        "vps_" + prefix,
		RequestedWindow: evidence.TimeWindow{Start: now.Add(-time.Hour), End: now},
	}
	// The resolver supplies the existing test authority scope only. The adapter
	// must load live PostgreSQL report/children, never a copied evidence DTO.
	adapter, err := adapters.NewIPQualityAdapter(repository, integrationEvidenceResolver{sourceID: selection.SourceID}, adapters.AdapterOptions{
		Clock: func() time.Time { return now }, NewIntentID: integrationEvidenceIntentID,
	})
	if err != nil {
		t.Fatalf("NewIPQualityAdapter(): %v", err)
	}
	preview, err := adapter.PreviewCapture(ctx, actor, selection)
	if err != nil {
		t.Fatalf("PreviewCapture(): %v", err)
	}
	digest, err := evidence.CapturePreviewDigest(preview)
	if err != nil {
		t.Fatalf("CapturePreviewDigest(): %v", err)
	}
	intent := evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: digest, ValidUntil: preview.ValidUntil}
	snapshot, err := adapter.Capture(ctx, actor, intent)
	if err != nil {
		t.Fatalf("Capture(): %v", err)
	}
	assertIPQualityProductEvidenceAgentFacts(t, snapshot, reportID, report.ReceivedAt)
	if snapshot.Envelope().Quality.Status != evidence.QualityPartial {
		t.Fatalf("mail-timeout evidence quality = %#v, want partial with provider facts", snapshot.Envelope().Quality)
	}
	capture, err := evidence.PrepareCapture(recordID, snapshotID, adapter.Descriptor(), preview, intent, snapshot.Envelope().Authorization, snapshot)
	if err != nil {
		t.Fatalf("PrepareCapture(): %v", err)
	}
	evidenceRepository := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture)
	preparation := storeEvidenceRevisionPreparation(t, recordID, []evidence.PreparedCapture{capture}, nil, []string{snapshotID})
	recordRepository := newRecordsPostgresRepository(t, runtimePool, NewRecordEvidenceRevisionParticipant())
	if _, err := recordRepository.CommitRevision(ctx, recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0,
		"Agent IP quality evidence", "ip-quality-product-evidence-boundary", preparation,
	)); err != nil {
		t.Fatalf("CommitRevision(evidence): %v", err)
	}
	assertPersistedEvidenceSnapshotMatchesCapture(t, ctx, fixture, capture)
	before := readIPQualityProductEvidencePayload(t, ctx, repository, recordID, snapshotID)
	if !bytes.Equal(before, snapshot.Bytes()) {
		t.Fatal("committed evidence payload differs from the adapter capture")
	}

	claim, err := repository.ClaimIPQualityIntelligence(ctx, "evidence-boundary-worker")
	if err != nil || claim == nil || claim.ReportID != reportID {
		t.Fatalf("ClaimIPQualityIntelligence() = %#v, %v, want %s", claim, err, reportID)
	}
	// A successful authenticated result deliberately disagrees with the Agent
	// row. It belongs in the product, not in v1's immutable Agent evidence.
	intelligence := ipquality.IntelligenceSnapshot{
		State: "complete", CredentialFingerprints: map[string]string{"ipapi.is": intelligenceFingerprint(credential)},
		Providers: []ipquality.ProviderResultRead{{
			Provider: "ipapi.is", Status: "success", SourceType: "api", IsVPN: new(false), IsTor: new(true),
			Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: report.IPAddress, ObservedAt: now, Method: "center_api", APITier: "authenticated",
				CountryCode: "JP", City: "Center-only city", FieldStates: map[string]string{"is_vpn": "known", "is_tor": "known"},
			},
		}},
		Blacklists: []agentapi.IPQualityBlacklistResult{{
			Source: "spamcop", Category: "spam", Status: "listed", QueryMethod: "dnsbl",
			Codes: []string{"127.0.0.2"}, ObservedAt: now,
		}},
		Registration: agentapi.IPQualityRegistration{
			Status: "success", StartAddress: report.IPAddress, EndAddress: report.IPAddress,
			NetworkCountryCode: "JP", NetworkName: "Center-only registration",
		},
		ASN: agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
		SourceOutcomes: []ipquality.SourceOutcome{
			{SourceID: "ipapi.is", Status: "success", ObservedAt: now},
			{SourceID: "spamcop", Status: "success", ObservedAt: now},
			{SourceID: "rdap-ip", Status: "success", ObservedAt: now},
		},
	}
	if err := repository.CompleteIPQualityIntelligence(ctx, *claim, intelligence); err != nil {
		t.Fatalf("CompleteIPQualityIntelligence(): %v", err)
	}
	page, err = repository.GetVPSIPQuality(ctx, selection.SourceID, 4)
	if err != nil {
		t.Fatalf("GetVPSIPQuality(complete): %v", err)
	}
	if page.Product == nil || page.Product.State != "complete" || page.Product.CompletedAt == nil || len(page.Product.Providers) != 2 ||
		page.Product.Providers[1].Details == nil || page.Product.Providers[1].Details.Method != "center_api" ||
		page.Product.Providers[1].IsVPN == nil || *page.Product.Providers[1].IsVPN ||
		page.Product.Providers[1].IsTor == nil || !*page.Product.Providers[1].IsTor ||
		page.Product.Registration.NetworkName != "Center-only registration" ||
		page.Product.Summary.BlacklistListedCount != 1 {
		t.Fatalf("finalized product did not expose actual Center enhancements: %#v", page.Product)
	}

	// Re-read the committed snapshot and payload after finalization. Checking
	// only the original in-memory snapshot would not detect a persistence bug.
	assertPersistedEvidenceSnapshotMatchesCapture(t, ctx, fixture, capture)
	after := readIPQualityProductEvidencePayload(t, ctx, repository, recordID, snapshotID)
	if !bytes.Equal(after, before) || evidence.CanonicalPayloadDigest(after) != snapshot.Hash() {
		t.Fatal("Center finalization changed the captured canonical payload or hash")
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, recordID, 1, 1, 1, 0)

	// A fresh capture through the real source must also remain Agent-only;
	// immutability must not merely be an accident of the persisted old row.
	laterAdapter, err := adapters.NewIPQualityAdapter(repository, integrationEvidenceResolver{sourceID: selection.SourceID}, adapters.AdapterOptions{
		Clock: func() time.Time { return now.Add(time.Minute) }, NewIntentID: integrationEvidenceIntentID,
	})
	if err != nil {
		t.Fatalf("NewIPQualityAdapter(after finalization): %v", err)
	}
	later := captureIntegrationEvidence(t, ctx, laterAdapter, actor, selection)
	assertIPQualityProductEvidenceAgentFacts(t, later, reportID, report.ReceivedAt)
	if !bytes.Equal(later.Bytes(), snapshot.Bytes()) || later.Hash() != snapshot.Hash() ||
		later.Envelope().SourceDigest != snapshot.Envelope().SourceDigest {
		t.Fatal("fresh Records v1 capture silently incorporated Center enhancements")
	}
}

func assertIPQualityProductEvidenceAgentFacts(t *testing.T, snapshot evidence.CanonicalSnapshot, reportID string, receivedAt time.Time) {
	t.Helper()
	var document struct {
		Payload struct {
			ReportID   string `json:"report_id"`
			ReceivedAt string `json:"received_at"`
			Status     string `json:"status"`
			RiskLevel  string `json:"risk_level"`
			Providers  []struct {
				Provider  string `json:"provider"`
				Status    string `json:"status"`
				RiskLevel string `json:"risk_level"`
				IsVPN     *bool  `json:"is_vpn"`
				IsTor     *bool  `json:"is_tor"`
			} `json:"providers"`
			Services []struct {
				Service     string `json:"service"`
				Status      string `json:"status"`
				ProbeStatus string `json:"probe_status"`
			} `json:"services"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(snapshot.Bytes(), &document); err != nil {
		t.Fatalf("decode v1 evidence: %v", err)
	}
	payload := document.Payload
	if payload.ReportID != reportID || payload.ReceivedAt != receivedAt.UTC().Format(time.RFC3339Nano) || payload.Status != "partial" || payload.RiskLevel != "" ||
		len(payload.Providers) != 1 || payload.Providers[0].Provider != "ipapi.is" || payload.Providers[0].Status != "success" ||
		payload.Providers[0].RiskLevel != "" || payload.Providers[0].IsVPN == nil || !*payload.Providers[0].IsVPN || payload.Providers[0].IsTor != nil ||
		len(payload.Services) != 1 || payload.Services[0].Service != "netflix" || payload.Services[0].Status != "unlocked" || payload.Services[0].ProbeStatus != "success" {
		t.Fatalf("v1 evidence lost or replaced known Agent facts: %s", snapshot.Bytes())
	}
	envelope := snapshot.Envelope()
	if envelope.Key != evidence.IPQualityReportV1Key() || envelope.CalculationVersion != "ip-quality-evidence/v1" ||
		envelope.SourceRevision != reportID || envelope.SourceWatermark != receivedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("v1 identity or Agent received-at watermark changed: %#v", envelope)
	}
	for _, forbidden := range []string{
		`"product"`, `"details"`, `"checks"`, `"mail"`, `"ptr_names"`, `"blacklists"`, `"registration"`, `"source_outcomes"`, `"scores"`,
		"center_api", "Center-only city", "Center-only registration", "127.0.0.2", "greeting_timeout",
	} {
		if bytes.Contains(snapshot.Bytes(), []byte(forbidden)) {
			t.Fatalf("Records v1 included product-only evidence %q: %s", forbidden, snapshot.Bytes())
		}
	}
}

func readIPQualityProductEvidencePayload(t *testing.T, ctx context.Context, repository *PostgresIPQualityRepository, recordID, snapshotID string) []byte {
	t.Helper()
	var compressed, canonicalHash []byte
	if err := repository.pool.QueryRow(ctx, `
		select p.compressed_payload, s.canonical_hash
		from evidence_snapshots s
		join evidence_payloads p on p.payload_digest = s.payload_digest
		where s.record_id = $1 and s.snapshot_id = $2`, recordID, snapshotID,
	).Scan(&compressed, &canonicalHash); err != nil {
		t.Fatalf("read captured payload through runtime role: %v", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("open captured payload: %v", err)
	}
	payload, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("decode captured payload: read=%v close=%v", readErr, closeErr)
	}
	digest := evidence.CanonicalPayloadDigest(payload)
	if !reflect.DeepEqual(canonicalHash, digest[:]) {
		t.Fatal("persisted snapshot hash does not match persisted payload")
	}
	return payload
}
