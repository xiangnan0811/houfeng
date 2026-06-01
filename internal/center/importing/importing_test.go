package importing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/providers"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestDecodeRecordsRejectsUnknownFields(t *testing.T) {
	_, err := DecodeRecords(strings.NewReader(`[{"display_name":"tokyo","unknown":true}]`))
	if err == nil {
		t.Fatal("DecodeRecords() error = nil, want non-nil for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeRecords() error = %v, want unknown field", err)
	}
}

func TestDryRunReportsValidationAndDecisionSignals(t *testing.T) {
	renewSoon := "2026-05-20"
	missingRenew := ""
	records := []InputRecord{
		{
			DisplayName:            " tokyo-01 ",
			ProviderName:           " Example Provider ",
			Country:                " Japan ",
			Region:                 " Tokyo ",
			City:                   " Tokyo ",
			IPv4:                   "192.0.2.10",
			SSHPort:                22,
			LifecycleStatus:        vpsassets.LifecycleActive,
			UsageStatus:            vpsassets.UsageIdle,
			Labels:                 []string{" proxy ", "proxy", ""},
			MonitoringInstanceName: "tokyo-monitoringInstance",
			TargetURL:              "https://tokyo.example",
			Subscription: &SubscriptionInput{
				Price:         36,
				Currency:      " usd ",
				BillingMonths: 12,
				RenewAt:       &renewSoon,
				Status:        subscriptions.StatusActive,
			},
		},
		{
			DisplayName:     "tokyo-01",
			ProviderName:    "Example Provider",
			IPv4:            "192.0.2.10",
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     vpsassets.UsageInUse,
			Subscription: &SubscriptionInput{
				Price:         10.123,
				Currency:      "US1",
				BillingMonths: 0,
				RenewAt:       &missingRenew,
			},
		},
		{
			DisplayName:     "orphan",
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     vpsassets.UsageInUse,
		},
	}

	report, err := DryRun(context.Background(), records, Repositories{
		Providers:           &fakeProviderRepo{},
		VPSAssets:           &fakeVPSRepo{},
		MonitoringInstances: fakeMonitoringInstanceRepo{records: []monitoringinstances.Record{{MonitoringInstanceID: "mi_1", DisplayName: "tokyo-monitoringInstance"}}},
	}, Options{Now: func() time.Time {
		return time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}

	if report.Mode != ModeDryRun {
		t.Fatalf("Mode = %q, want %q", report.Mode, ModeDryRun)
	}
	if report.CanImport {
		t.Fatal("CanImport = true, want false when validation and duplicates exist")
	}
	if report.Totals.InputRows != 3 {
		t.Fatalf("InputRows = %d, want 3", report.Totals.InputRows)
	}
	if len(report.ProviderCandidates) != 1 || report.ProviderCandidates[0].Name != "Example Provider" {
		t.Fatalf("ProviderCandidates = %#v, want Example Provider", report.ProviderCandidates)
	}
	if len(report.MissingProviderRows) != 1 || report.MissingProviderRows[0].Row != 3 {
		t.Fatalf("MissingProviderRows = %#v, want row 3", report.MissingProviderRows)
	}
	if len(report.MissingRenewDateRows) != 1 || report.MissingRenewDateRows[0].Row != 2 {
		t.Fatalf("MissingRenewDateRows = %#v, want row 2", report.MissingRenewDateRows)
	}
	if len(report.ValidationErrors) == 0 {
		t.Fatal("ValidationErrors empty, want subscription validation error")
	}
	if report.ValidationErrors[0].Row != 2 || report.ValidationErrors[0].Field != "subscription" {
		t.Fatalf("ValidationErrors = %#v, want row 2 subscription validation error", report.ValidationErrors)
	}
	if len(report.DuplicateCandidates) == 0 {
		t.Fatal("DuplicateCandidates empty, want input duplicate candidates")
	}
	if len(report.MonitoringInstanceAssociationCandidates) != 1 || !strings.Contains(report.MonitoringInstanceAssociationCandidates[0].Status, "monitoring_instance_name matches") {
		t.Fatalf("MonitoringInstanceAssociationCandidates = %#v, want monitoring_instance_name match", report.MonitoringInstanceAssociationCandidates)
	}
	if len(report.RenewalCandidates) != 1 || report.RenewalCandidates[0].DaysUntil != 11 {
		t.Fatalf("RenewalCandidates = %#v, want 11 day candidate", report.RenewalCandidates)
	}
	if len(report.IdlePaidCandidates) != 1 || report.IdlePaidCandidates[0].DisplayName != "tokyo-01" {
		t.Fatalf("IdlePaidCandidates = %#v, want tokyo-01", report.IdlePaidCandidates)
	}
}

func TestDryRunReportsExistingDuplicates(t *testing.T) {
	report, err := DryRun(context.Background(), []InputRecord{{
		DisplayName:     "Tokyo",
		ProviderName:    "Existing Provider",
		OrderRef:        "order-1",
		IPv4:            "192.0.2.10",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	}}, Repositories{
		Providers: &fakeProviderRepo{records: []providers.Record{{ProviderID: "pv_1", Name: "Existing Provider"}}},
		VPSAssets: &fakeVPSRepo{records: []vpsassets.Record{{
			VPSID:           "vps_1",
			DisplayName:     "Tokyo",
			ProviderID:      stringPtrForTest("pv_1"),
			OrderRef:        "order-1",
			IPv4:            "192.0.2.10",
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     vpsassets.UsageInUse,
		}}},
	}, Options{})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if len(report.DuplicateCandidates) == 0 {
		t.Fatal("DuplicateCandidates empty, want existing duplicate candidates")
	}
	if report.CanImport {
		t.Fatal("CanImport = true, want false with duplicate candidates")
	}
}

func TestDryRunCanIgnoreRepositoryErrors(t *testing.T) {
	report, err := DryRun(context.Background(), []InputRecord{{
		DisplayName:     "Tokyo",
		ProviderName:    "Example Provider",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	}}, Repositories{
		Providers: &fakeProviderRepo{listErr: errors.New("providers missing")},
		VPSAssets: &fakeVPSRepo{},
	}, Options{IgnoreRepositoryErrors: true})
	if err != nil {
		t.Fatalf("DryRun() error = %v, want nil when repository errors are ignored", err)
	}
	if report.DatabaseChecked {
		t.Fatal("DatabaseChecked = true, want false after partial database check failure")
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "provider database check skipped") {
		t.Fatalf("Warnings = %#v, want provider database warning", report.Warnings)
	}
	if report.Totals.ProviderCreateCandidates != 1 {
		t.Fatalf("ProviderCreateCandidates = %d, want 1 after fallback to file-only provider analysis", report.Totals.ProviderCreateCandidates)
	}
}

func TestImportCreatesRecordsWhenDryRunIsClean(t *testing.T) {
	renewAt := "2026-06-01"
	providerRepo := &fakeProviderRepo{}
	vpsRepo := &fakeVPSRepo{}
	subscriptionRepo := &fakeSubscriptionRepo{}

	report, err := Import(context.Background(), []InputRecord{{
		DisplayName:     "Tokyo",
		ProviderName:    "Example Provider",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		Subscription: &SubscriptionInput{
			Price:         120,
			Currency:      "usd",
			BillingMonths: 12,
			RenewAt:       &renewAt,
		},
	}}, Repositories{
		Providers:     providerRepo,
		VPSAssets:     vpsRepo,
		Subscriptions: subscriptionRepo,
	}, Options{Now: func() time.Time {
		return time.Date(2026, time.May, 9, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if !report.CanImport {
		t.Fatal("CanImport = false, want true")
	}
	if len(providerRepo.created) != 1 || providerRepo.created[0].Name != "Example Provider" {
		t.Fatalf("created providers = %#v, want Example Provider", providerRepo.created)
	}
	if len(vpsRepo.created) != 1 {
		t.Fatalf("created vps = %#v, want one", vpsRepo.created)
	}
	if vpsRepo.created[0].ProviderID == nil || *vpsRepo.created[0].ProviderID != "pv_created_1" {
		t.Fatalf("created vps provider_id = %#v, want pv_created_1", vpsRepo.created[0].ProviderID)
	}
	if len(subscriptionRepo.created) != 1 || subscriptionRepo.created[0].VPSID != "vps_created_1" {
		t.Fatalf("created subscriptions = %#v, want vps_created_1", subscriptionRepo.created)
	}
	if report.Totals.ImportedProviders != 1 || report.Totals.ImportedVPSAssets != 1 || report.Totals.ImportedSubscriptions != 1 {
		t.Fatalf("import totals = %#v, want 1/1/1", report.Totals)
	}
}

func TestImportBlocksValidationErrorsAndDuplicates(t *testing.T) {
	_, err := Import(context.Background(), []InputRecord{{
		DisplayName:     "broken",
		ProviderName:    "Example",
		LifecycleStatus: "bad",
		UsageStatus:     vpsassets.UsageInUse,
	}}, Repositories{
		Providers:     &fakeProviderRepo{},
		VPSAssets:     &fakeVPSRepo{},
		Subscriptions: &fakeSubscriptionRepo{},
	}, Options{})
	if !errors.Is(err, ErrImportBlocked) {
		t.Fatalf("Import() error = %v, want ErrImportBlocked", err)
	}
}

func TestWriteReportFormatsJSONAndText(t *testing.T) {
	report := Report{
		Mode:        ModeDryRun,
		CurrentDate: "2026-05-09",
		CanImport:   true,
		Totals:      Totals{InputRows: 1},
	}

	var text bytes.Buffer
	if err := WriteReport(&text, report, "text"); err != nil {
		t.Fatalf("WriteReport(text) error = %v", err)
	}
	if !strings.Contains(text.String(), "VPS JSON import report") {
		t.Fatalf("text report = %q, want heading", text.String())
	}

	var raw bytes.Buffer
	if err := WriteReport(&raw, report, "json"); err != nil {
		t.Fatalf("WriteReport(json) error = %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(raw.Bytes(), &decoded); err != nil {
		t.Fatalf("json report invalid: %v\n%s", err, raw.String())
	}
	if decoded.CurrentDate != "2026-05-09" {
		t.Fatalf("decoded.CurrentDate = %q, want 2026-05-09", decoded.CurrentDate)
	}
}

type fakeProviderRepo struct {
	records []providers.Record
	created []providers.CreateInput
	listErr error
}

func (f *fakeProviderRepo) ListProviders(context.Context) ([]providers.Record, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]providers.Record(nil), f.records...), nil
}

func (f *fakeProviderRepo) CreateProvider(_ context.Context, input providers.CreateInput) (providers.Record, error) {
	input = providers.NormalizeCreateInput(input)
	f.created = append(f.created, input)
	return providers.Record{ProviderID: "pv_created_1", Name: input.Name}, nil
}

type fakeVPSRepo struct {
	records []vpsassets.Record
	created []vpsassets.CreateInput
}

func (f *fakeVPSRepo) ListVPSAssets(context.Context, vpsassets.ListFilters) ([]vpsassets.Record, error) {
	return append([]vpsassets.Record(nil), f.records...), nil
}

func (f *fakeVPSRepo) CreateVPSAsset(_ context.Context, input vpsassets.CreateInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizeCreateInput(input)
	f.created = append(f.created, input)
	return vpsassets.Record{VPSID: "vps_created_1", DisplayName: input.DisplayName, ProviderID: input.ProviderID}, nil
}

type fakeSubscriptionRepo struct {
	created []subscriptions.CreateInput
}

func (f *fakeSubscriptionRepo) ListSubscriptions(context.Context, subscriptions.ListFilters) ([]subscriptions.Record, error) {
	return nil, nil
}

func (f *fakeSubscriptionRepo) CreateSubscription(_ context.Context, input subscriptions.CreateInput) (subscriptions.Record, error) {
	input = subscriptions.NormalizeCreateInput(input)
	f.created = append(f.created, input)
	return subscriptions.Record{SubscriptionID: "sub_created_1", VPSID: input.VPSID}, nil
}

type fakeMonitoringInstanceRepo struct {
	records []monitoringinstances.Record
}

func (f fakeMonitoringInstanceRepo) ListMonitoringInstances(context.Context) ([]monitoringinstances.Record, error) {
	return append([]monitoringinstances.Record(nil), f.records...), nil
}

func stringPtrForTest(value string) *string {
	return &value
}
