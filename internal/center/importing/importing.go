package importing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/providers"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

const (
	ModeDryRun = "dry-run"
	ModeImport = "import"
)

type preparedRecord struct {
	Row               int
	Input             InputRecord
	ProviderID        *string
	ProviderName      string
	VPSInput          vpsassets.CreateInput
	SubscriptionInput *subscriptions.CreateInput
}

type existingState struct {
	ProvidersByID           map[string]providers.Record
	ProvidersByName         map[string]providers.Record
	VPSAssets               []vpsassets.Record
	MonitoringInstanceIDs   map[string]struct{}
	MonitoringInstanceNames map[string]struct{}
	DatabaseChecked         bool
}

func DecodeRecords(r io.Reader) ([]InputRecord, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	var records []InputRecord
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("decode vps import json: %w", err)
	}

	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode vps import json: trailing data")
		}
		return nil, fmt.Errorf("decode vps import json: %w", err)
	}
	return records, nil
}

func DryRun(ctx context.Context, records []InputRecord, repos Repositories, opts Options) (Report, error) {
	report, _, err := analyze(ctx, records, repos, opts)
	if err != nil {
		return Report{}, err
	}
	report.Mode = ModeDryRun
	return report, nil
}

func (r *Report) AddWarning(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	r.Warnings = append(r.Warnings, message)
}

func Import(ctx context.Context, records []InputRecord, repos Repositories, opts Options) (Report, error) {
	if repos.Providers == nil || repos.VPSAssets == nil || repos.Subscriptions == nil {
		return Report{}, fmt.Errorf("%w: import requires provider, vps asset, and subscription repositories", ErrImportBlocked)
	}

	report, prepared, err := analyze(ctx, records, repos, opts)
	if err != nil {
		return Report{}, err
	}
	report.Mode = ModeImport
	if !report.CanImport {
		return report, ErrImportBlocked
	}

	createdProviderIDs := make(map[string]string, len(report.ProviderCandidates))
	for _, candidate := range report.ProviderCandidates {
		record, err := repos.Providers.CreateProvider(ctx, providers.CreateInput{Name: candidate.Name})
		if err != nil {
			return report, fmt.Errorf("create provider %q: %w", candidate.Name, err)
		}
		createdProviderIDs[canonicalKey(candidate.Name)] = record.ProviderID
		report.Import.CreatedProviders = append(report.Import.CreatedProviders, CreatedProvider{
			ProviderID: record.ProviderID,
			Name:       record.Name,
		})
	}

	createdVPSIDs := make(map[int]string, len(prepared))
	for _, record := range prepared {
		input := record.VPSInput
		if input.ProviderID == nil && record.ProviderName != "" {
			if providerID, ok := createdProviderIDs[canonicalKey(record.ProviderName)]; ok {
				input.ProviderID = stringPtr(providerID)
			}
		}

		created, err := repos.VPSAssets.CreateVPSAsset(ctx, input)
		if err != nil {
			return report, fmt.Errorf("create vps asset for row %d: %w", record.Row, err)
		}
		createdVPSIDs[record.Row] = created.VPSID
		report.Import.CreatedVPSAssets = append(report.Import.CreatedVPSAssets, CreatedVPSAsset{
			Row:         record.Row,
			VPSID:       created.VPSID,
			DisplayName: created.DisplayName,
		})
	}

	for _, record := range prepared {
		if record.SubscriptionInput == nil {
			continue
		}
		vpsID := createdVPSIDs[record.Row]
		input := *record.SubscriptionInput
		input.VPSID = vpsID

		created, err := repos.Subscriptions.CreateSubscription(ctx, input)
		if err != nil {
			return report, fmt.Errorf("create subscription for row %d: %w", record.Row, err)
		}
		report.Import.CreatedSubscriptions = append(report.Import.CreatedSubscriptions, CreatedSubscription{
			Row:            record.Row,
			SubscriptionID: created.SubscriptionID,
			VPSID:          created.VPSID,
		})
	}

	report.Totals.ImportedProviders = len(report.Import.CreatedProviders)
	report.Totals.ImportedVPSAssets = len(report.Import.CreatedVPSAssets)
	report.Totals.ImportedSubscriptions = len(report.Import.CreatedSubscriptions)
	return report, nil
}

func analyze(ctx context.Context, records []InputRecord, repos Repositories, opts Options) (Report, []preparedRecord, error) {
	now := importNow(opts)
	state, warnings, err := loadExistingState(ctx, repos, opts)
	if err != nil {
		return Report{}, nil, err
	}

	report := Report{
		CurrentDate:     dateString(now),
		DatabaseChecked: state.DatabaseChecked,
		Warnings:        warnings,
		Totals:          Totals{InputRows: len(records)},
	}

	prepared := make([]preparedRecord, 0, len(records))
	providerRows := make(map[string][]int)
	providerNames := make(map[string]string)
	inputDuplicateRows := map[string]map[string][]int{
		"vps_natural_key": {},
		"order_ref":       {},
		"ipv4":            {},
		"ssh_host":        {},
	}
	existingDuplicateKeys := make(map[string]struct{})

	for index, input := range records {
		row := index + 1
		record := prepareInputRecord(row, input, state, &report)
		prepared = append(prepared, record)

		if record.ProviderID == nil && record.ProviderName == "" {
			report.MissingProviderRows = append(report.MissingProviderRows, RowIssue{
				Row:     row,
				Field:   "provider_name",
				Message: "provider_id or provider_name is required to resolve provider identity",
			})
		}
		if record.ProviderID == nil && record.ProviderName != "" {
			key := canonicalKey(record.ProviderName)
			if _, exists := state.ProvidersByName[key]; !exists {
				providerRows[key] = append(providerRows[key], row)
				providerNames[key] = record.ProviderName
			}
		}

		if err := providers.ValidateCreateInput(providers.NormalizeCreateInput(providers.CreateInput{Name: record.ProviderName})); record.ProviderName != "" && err != nil {
			report.ValidationErrors = append(report.ValidationErrors, RowIssue{Row: row, Field: "provider_name", Message: err.Error()})
		}
		if record.ProviderID != nil && state.DatabaseChecked {
			if _, ok := state.ProvidersByID[*record.ProviderID]; !ok {
				report.ValidationErrors = append(report.ValidationErrors, RowIssue{
					Row:     row,
					Field:   "provider_id",
					Message: "provider_id does not exist",
				})
			}
		}
		if err := vpsassets.ValidateCreateInput(record.VPSInput); err != nil {
			report.ValidationErrors = append(report.ValidationErrors, RowIssue{Row: row, Field: "vps", Message: err.Error()})
		}
		if record.SubscriptionInput != nil {
			if err := subscriptions.ValidateCreateInput(*record.SubscriptionInput); err != nil {
				report.ValidationErrors = append(report.ValidationErrors, RowIssue{Row: row, Field: "subscription", Message: err.Error()})
			}
			if record.SubscriptionInput.RenewAt == nil {
				report.MissingRenewDateRows = append(report.MissingRenewDateRows, RowIssue{
					Row:     row,
					Field:   "subscription.renew_at",
					Message: "renew_at is missing",
				})
			}
		}

		report.VPSCandidates = append(report.VPSCandidates, vpsCandidateFromRecord(record))
		if record.SubscriptionInput != nil {
			report.SubscriptionCandidates = append(report.SubscriptionCandidates, subscriptionCandidateFromRecord(record))
			appendRenewalAndIdleCandidates(&report, record, now)
		}
		appendMonitoringInstanceCandidate(&report, record, state)
		trackInputDuplicates(inputDuplicateRows, record)
		appendExistingDuplicates(&report, record, state, existingDuplicateKeys)
	}

	report.ProviderCandidates = providerCandidates(providerRows, providerNames)
	appendInputDuplicates(&report, inputDuplicateRows)
	sortReport(&report)
	report.Totals = reportTotals(report, len(records))
	report.CanImport = len(report.ValidationErrors) == 0 && len(report.DuplicateCandidates) == 0
	ensureReportCollections(&report)
	return report, prepared, nil
}

func loadExistingState(ctx context.Context, repos Repositories, opts Options) (existingState, []string, error) {
	state := existingState{
		ProvidersByID:           map[string]providers.Record{},
		ProvidersByName:         map[string]providers.Record{},
		MonitoringInstanceIDs:   map[string]struct{}{},
		MonitoringInstanceNames: map[string]struct{}{},
	}
	warnings := []string{}
	checkRequested := false
	checkFailed := false

	if repos.Providers != nil {
		checkRequested = true
		records, err := repos.Providers.ListProviders(ctx)
		if err != nil {
			if !opts.IgnoreRepositoryErrors {
				return state, warnings, fmt.Errorf("list providers for import analysis: %w", err)
			}
			warnings = append(warnings, fmt.Sprintf("provider database check skipped: %v", err))
			checkFailed = true
			records = nil
		}
		for _, record := range records {
			state.ProvidersByID[record.ProviderID] = record
			state.ProvidersByName[canonicalKey(record.Name)] = record
		}
	}
	if repos.VPSAssets != nil {
		checkRequested = true
		records, err := repos.VPSAssets.ListVPSAssets(ctx, vpsassets.ListFilters{})
		if err != nil {
			if !opts.IgnoreRepositoryErrors {
				return state, warnings, fmt.Errorf("list vps assets for import analysis: %w", err)
			}
			warnings = append(warnings, fmt.Sprintf("vps asset database check skipped: %v", err))
			checkFailed = true
			records = nil
		}
		state.VPSAssets = records
	}
	if repos.Subscriptions != nil {
		checkRequested = true
		if _, err := repos.Subscriptions.ListSubscriptions(ctx, subscriptions.ListFilters{}); err != nil {
			if !opts.IgnoreRepositoryErrors {
				return state, warnings, fmt.Errorf("list subscriptions for import analysis: %w", err)
			}
			warnings = append(warnings, fmt.Sprintf("subscription database check skipped: %v", err))
			checkFailed = true
		}
	}
	if repos.MonitoringInstances != nil {
		checkRequested = true
		records, err := repos.MonitoringInstances.ListMonitoringInstances(ctx)
		if err != nil {
			if !opts.IgnoreRepositoryErrors {
				return state, warnings, fmt.Errorf("list monitoring instances for import analysis: %w", err)
			}
			warnings = append(warnings, fmt.Sprintf("monitoring instance database check skipped: %v", err))
			checkFailed = true
			records = nil
		}
		for _, record := range records {
			state.MonitoringInstanceIDs[record.MonitoringInstanceID] = struct{}{}
			state.MonitoringInstanceNames[canonicalKey(record.DisplayName)] = struct{}{}
		}
	}
	state.DatabaseChecked = checkRequested && !checkFailed
	return state, warnings, nil
}

func prepareInputRecord(row int, input InputRecord, state existingState, report *Report) preparedRecord {
	providerID := normalizeStringPtr(input.ProviderID)
	providerName := strings.TrimSpace(input.ProviderName)
	if providerID == nil && providerName != "" {
		if existing, ok := state.ProvidersByName[canonicalKey(providerName)]; ok {
			providerID = stringPtr(existing.ProviderID)
		}
	}

	vpsInput := vpsassets.NormalizeCreateInput(vpsassets.CreateInput{
		DisplayName:     input.DisplayName,
		ProviderID:      providerID,
		ProviderName:    providerName,
		ProductName:     input.ProductName,
		OrderRef:        input.OrderRef,
		Country:         input.Country,
		Region:          input.Region,
		City:            input.City,
		Datacenter:      input.Datacenter,
		IPv4:            input.IPv4,
		IPv6:            input.IPv6,
		SSHHost:         input.SSHHost,
		SSHPort:         input.SSHPort,
		SSHUser:         input.SSHUser,
		OSName:          input.OSName,
		Virtualization:  input.Virtualization,
		LifecycleStatus: input.LifecycleStatus,
		UsageStatus:     input.UsageStatus,
		RenewalDecision: input.RenewalDecision,
		Importance:      input.Importance,
		Labels:          input.Labels,
		Note:            input.Note,
	})

	var subscriptionInput *subscriptions.CreateInput
	if input.Subscription != nil {
		startedAt := parseOptionalDate(row, "subscription.started_at", input.Subscription.StartedAt, report)
		renewAt := parseOptionalDate(row, "subscription.renew_at", input.Subscription.RenewAt, report)
		normalized := subscriptions.NormalizeCreateInput(subscriptions.CreateInput{
			VPSID:              placeholderVPSID(row),
			Price:              input.Subscription.Price,
			Currency:           input.Subscription.Currency,
			BillingCycle:       input.Subscription.BillingCycle,
			BillingMonths:      input.Subscription.BillingMonths,
			StartedAt:          startedAt,
			RenewAt:            renewAt,
			AutoRenew:          input.Subscription.AutoRenew,
			AutoRenewCancelled: input.Subscription.AutoRenewCancelled,
			Status:             input.Subscription.Status,
			PaymentMethod:      input.Subscription.PaymentMethod,
			Note:               input.Subscription.Note,
		})
		subscriptionInput = &normalized
	}

	input.MonitoringInstanceID = strings.TrimSpace(input.MonitoringInstanceID)
	input.MonitoringInstanceName = strings.TrimSpace(input.MonitoringInstanceName)
	input.AgentTokenHint = strings.TrimSpace(input.AgentTokenHint)
	input.TargetURL = strings.TrimSpace(input.TargetURL)

	return preparedRecord{
		Row:               row,
		Input:             input,
		ProviderID:        providerID,
		ProviderName:      providerName,
		VPSInput:          vpsInput,
		SubscriptionInput: subscriptionInput,
	}
}

func parseOptionalDate(row int, field string, value *string, report *Report) *subscriptions.Date {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	parsed, err := subscriptions.ParseDate(*value)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, RowIssue{
			Row:     row,
			Field:   field,
			Message: err.Error(),
		})
		return nil
	}
	return &parsed
}

func providerCandidates(rowsByName map[string][]int, names map[string]string) []ProviderCandidate {
	keys := make([]string, 0, len(rowsByName))
	for key := range rowsByName {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	candidates := make([]ProviderCandidate, 0, len(keys))
	for _, key := range keys {
		rows := append([]int(nil), rowsByName[key]...)
		sort.Ints(rows)
		candidates = append(candidates, ProviderCandidate{Name: names[key], Rows: rows})
	}
	return candidates
}

func vpsCandidateFromRecord(record preparedRecord) VPSCandidate {
	return VPSCandidate{
		Row:          record.Row,
		DisplayName:  record.VPSInput.DisplayName,
		ProviderID:   cloneStringPtr(record.ProviderID),
		ProviderName: record.ProviderName,
		Country:      record.VPSInput.Country,
		Region:       record.VPSInput.Region,
		City:         record.VPSInput.City,
	}
}

func subscriptionCandidateFromRecord(record preparedRecord) SubscriptionCandidate {
	input := record.SubscriptionInput
	candidate := SubscriptionCandidate{
		Row:           record.Row,
		DisplayName:   record.VPSInput.DisplayName,
		Price:         input.Price,
		Currency:      input.Currency,
		BillingMonths: input.BillingMonths,
		MonthlyPrice:  subscriptions.CalculateMonthlyPrice(input.Price, input.BillingMonths),
	}
	if input.RenewAt != nil {
		value := input.RenewAt.Time.Format(subscriptions.DateLayout)
		candidate.RenewAt = &value
	}
	return candidate
}

func appendRenewalAndIdleCandidates(report *Report, record preparedRecord, now time.Time) {
	input := record.SubscriptionInput
	if input == nil {
		return
	}
	if input.RenewAt != nil && isRenewalRelevant(input.Status) {
		days := int(input.RenewAt.Time.Sub(startOfDay(now)).Hours() / 24)
		if days >= 0 && days <= 30 {
			report.RenewalCandidates = append(report.RenewalCandidates, RenewalCandidate{
				Row:          record.Row,
				DisplayName:  record.VPSInput.DisplayName,
				RenewAt:      input.RenewAt.Time.Format(subscriptions.DateLayout),
				DaysUntil:    days,
				Price:        input.Price,
				Currency:     input.Currency,
				MonthlyPrice: subscriptions.CalculateMonthlyPrice(input.Price, input.BillingMonths),
			})
		}
	}
	if record.VPSInput.UsageStatus == vpsassets.UsageIdle && input.Status == subscriptions.StatusActive && input.Price > 0 {
		candidate := IdlePaidCandidate{
			Row:          record.Row,
			DisplayName:  record.VPSInput.DisplayName,
			Price:        input.Price,
			Currency:     input.Currency,
			MonthlyPrice: subscriptions.CalculateMonthlyPrice(input.Price, input.BillingMonths),
		}
		if input.RenewAt != nil {
			value := input.RenewAt.Time.Format(subscriptions.DateLayout)
			candidate.RenewAt = &value
		}
		report.IdlePaidCandidates = append(report.IdlePaidCandidates, candidate)
	}
}

func appendMonitoringInstanceCandidate(report *Report, record preparedRecord, state existingState) {
	if record.Input.MonitoringInstanceID == "" && record.Input.MonitoringInstanceName == "" && record.Input.AgentTokenHint == "" && record.Input.TargetURL == "" {
		return
	}

	status := "manual confirmation required"
	if state.DatabaseChecked {
		switch {
		case record.Input.MonitoringInstanceID != "":
			if _, ok := state.MonitoringInstanceIDs[record.Input.MonitoringInstanceID]; ok {
				status = "monitoring_instance_id exists; manual confirmation required"
			} else {
				status = "monitoring_instance_id not found"
			}
		case record.Input.MonitoringInstanceName != "":
			if _, ok := state.MonitoringInstanceNames[canonicalKey(record.Input.MonitoringInstanceName)]; ok {
				status = "monitoring_instance_name matches existing monitoring instance; manual confirmation required"
			} else {
				status = "monitoring_instance_name not found"
			}
		default:
			status = "database checked; no direct monitoring instance match key supplied"
		}
	}

	report.MonitoringInstanceAssociationCandidates = append(report.MonitoringInstanceAssociationCandidates, MonitoringInstanceAssociationCandidate{
		Row:                    record.Row,
		DisplayName:            record.VPSInput.DisplayName,
		MonitoringInstanceID:   record.Input.MonitoringInstanceID,
		MonitoringInstanceName: record.Input.MonitoringInstanceName,
		TargetURL:              record.Input.TargetURL,
		Status:                 status,
	})
}

func trackInputDuplicates(rowsByType map[string]map[string][]int, record preparedRecord) {
	addDuplicateRow(rowsByType["vps_natural_key"], vpsNaturalKey(record), record.Row)
	addDuplicateRow(rowsByType["order_ref"], canonicalField(record.VPSInput.OrderRef), record.Row)
	addDuplicateRow(rowsByType["ipv4"], canonicalField(record.VPSInput.IPv4), record.Row)
	addDuplicateRow(rowsByType["ssh_host"], canonicalField(record.VPSInput.SSHHost), record.Row)
}

func addDuplicateRow(rows map[string][]int, key string, row int) {
	if key == "" {
		return
	}
	rows[key] = append(rows[key], row)
}

func appendInputDuplicates(report *Report, rowsByType map[string]map[string][]int) {
	types := make([]string, 0, len(rowsByType))
	for typ := range rowsByType {
		types = append(types, typ)
	}
	sort.Strings(types)

	for _, typ := range types {
		keys := make([]string, 0, len(rowsByType[typ]))
		for key, rows := range rowsByType[typ] {
			if len(rows) > 1 {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			rows := append([]int(nil), rowsByType[typ][key]...)
			sort.Ints(rows)
			report.DuplicateCandidates = append(report.DuplicateCandidates, DuplicateCandidate{
				Type:    typ,
				Key:     key,
				Rows:    rows,
				Message: "duplicate candidate within input",
			})
		}
	}
}

func appendExistingDuplicates(report *Report, record preparedRecord, state existingState, seen map[string]struct{}) {
	if !state.DatabaseChecked {
		return
	}
	checks := []struct {
		typ string
		key string
		id  string
	}{
		{typ: "vps_natural_key", key: vpsNaturalKey(record), id: existingVPSIDByNaturalKey(state.VPSAssets, record)},
		{typ: "order_ref", key: canonicalField(record.VPSInput.OrderRef), id: existingVPSIDByField(state.VPSAssets, "order_ref", record.VPSInput.OrderRef)},
		{typ: "ipv4", key: canonicalField(record.VPSInput.IPv4), id: existingVPSIDByField(state.VPSAssets, "ipv4", record.VPSInput.IPv4)},
		{typ: "ssh_host", key: canonicalField(record.VPSInput.SSHHost), id: existingVPSIDByField(state.VPSAssets, "ssh_host", record.VPSInput.SSHHost)},
	}
	for _, check := range checks {
		if check.key == "" || check.id == "" {
			continue
		}
		seenKey := check.typ + ":" + check.key + ":" + check.id
		if _, ok := seen[seenKey]; ok {
			continue
		}
		seen[seenKey] = struct{}{}
		report.DuplicateCandidates = append(report.DuplicateCandidates, DuplicateCandidate{
			Type:       check.typ,
			Key:        check.key,
			Rows:       []int{record.Row},
			ExistingID: check.id,
			Message:    "duplicate candidate against existing data",
		})
	}
}

func existingVPSIDByNaturalKey(existing []vpsassets.Record, record preparedRecord) string {
	key := vpsNaturalKey(record)
	if key == "" {
		return ""
	}
	for _, candidate := range existing {
		if existingVPSNaturalKey(candidate) == key {
			return candidate.VPSID
		}
	}
	return ""
}

func existingVPSIDByField(existing []vpsassets.Record, field, value string) string {
	key := canonicalField(value)
	if key == "" {
		return ""
	}
	for _, candidate := range existing {
		var candidateValue string
		switch field {
		case "order_ref":
			candidateValue = candidate.OrderRef
		case "ipv4":
			candidateValue = candidate.IPv4
		case "ssh_host":
			candidateValue = candidate.SSHHost
		}
		if canonicalField(candidateValue) == key {
			return candidate.VPSID
		}
	}
	return ""
}

func reportTotals(report Report, inputRows int) Totals {
	totals := report.Totals
	totals.InputRows = inputRows
	totals.ProviderCreateCandidates = len(report.ProviderCandidates)
	totals.VPSCreateCandidates = len(report.VPSCandidates)
	totals.SubscriptionCandidates = len(report.SubscriptionCandidates)
	totals.MissingProviderRows = len(report.MissingProviderRows)
	totals.MissingRenewDateRows = len(report.MissingRenewDateRows)
	totals.ValidationErrors = len(report.ValidationErrors)
	totals.DuplicateCandidates = len(report.DuplicateCandidates)
	totals.MonitoringInstanceAssociationCandidates = len(report.MonitoringInstanceAssociationCandidates)
	totals.RenewalCandidates = len(report.RenewalCandidates)
	totals.IdlePaidCandidates = len(report.IdlePaidCandidates)
	totals.ImportedProviders = len(report.Import.CreatedProviders)
	totals.ImportedVPSAssets = len(report.Import.CreatedVPSAssets)
	totals.ImportedSubscriptions = len(report.Import.CreatedSubscriptions)
	return totals
}

func ensureReportCollections(report *Report) {
	if report.ProviderCandidates == nil {
		report.ProviderCandidates = []ProviderCandidate{}
	}
	if report.Warnings == nil {
		report.Warnings = []string{}
	}
	if report.VPSCandidates == nil {
		report.VPSCandidates = []VPSCandidate{}
	}
	if report.SubscriptionCandidates == nil {
		report.SubscriptionCandidates = []SubscriptionCandidate{}
	}
	if report.MissingProviderRows == nil {
		report.MissingProviderRows = []RowIssue{}
	}
	if report.MissingRenewDateRows == nil {
		report.MissingRenewDateRows = []RowIssue{}
	}
	if report.ValidationErrors == nil {
		report.ValidationErrors = []RowIssue{}
	}
	if report.DuplicateCandidates == nil {
		report.DuplicateCandidates = []DuplicateCandidate{}
	}
	if report.MonitoringInstanceAssociationCandidates == nil {
		report.MonitoringInstanceAssociationCandidates = []MonitoringInstanceAssociationCandidate{}
	}
	if report.RenewalCandidates == nil {
		report.RenewalCandidates = []RenewalCandidate{}
	}
	if report.IdlePaidCandidates == nil {
		report.IdlePaidCandidates = []IdlePaidCandidate{}
	}
	if report.Import.CreatedProviders == nil {
		report.Import.CreatedProviders = []CreatedProvider{}
	}
	if report.Import.CreatedVPSAssets == nil {
		report.Import.CreatedVPSAssets = []CreatedVPSAsset{}
	}
	if report.Import.CreatedSubscriptions == nil {
		report.Import.CreatedSubscriptions = []CreatedSubscription{}
	}
}

func sortReport(report *Report) {
	sort.Slice(report.MissingProviderRows, lessRowIssue(report.MissingProviderRows))
	sort.Slice(report.MissingRenewDateRows, lessRowIssue(report.MissingRenewDateRows))
	sort.Slice(report.ValidationErrors, lessRowIssue(report.ValidationErrors))
	sort.Slice(report.DuplicateCandidates, func(i, j int) bool {
		if report.DuplicateCandidates[i].Type != report.DuplicateCandidates[j].Type {
			return report.DuplicateCandidates[i].Type < report.DuplicateCandidates[j].Type
		}
		return report.DuplicateCandidates[i].Key < report.DuplicateCandidates[j].Key
	})
}

func lessRowIssue(issues []RowIssue) func(i, j int) bool {
	return func(i, j int) bool {
		if issues[i].Row != issues[j].Row {
			return issues[i].Row < issues[j].Row
		}
		return issues[i].Field < issues[j].Field
	}
}

func isRenewalRelevant(status subscriptions.Status) bool {
	return status != subscriptions.StatusCancelled && status != subscriptions.StatusExpired
}

func vpsNaturalKey(record preparedRecord) string {
	displayName := canonicalField(record.VPSInput.DisplayName)
	if displayName == "" {
		return ""
	}
	switch {
	case record.ProviderID != nil:
		return "provider_id=" + *record.ProviderID + "|display_name=" + displayName
	case record.ProviderName != "":
		return "provider_name=" + canonicalField(record.ProviderName) + "|display_name=" + displayName
	default:
		return "display_name=" + displayName
	}
}

func existingVPSNaturalKey(record vpsassets.Record) string {
	displayName := canonicalField(record.DisplayName)
	if displayName == "" {
		return ""
	}
	switch {
	case record.ProviderID != nil:
		return "provider_id=" + *record.ProviderID + "|display_name=" + displayName
	case record.ProviderName != "":
		return "provider_name=" + canonicalField(record.ProviderName) + "|display_name=" + displayName
	default:
		return "display_name=" + displayName
	}
}

func placeholderVPSID(row int) string {
	return fmt.Sprintf("import_row_%d", row)
}

func canonicalKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func canonicalField(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func stringPtr(value string) *string {
	return &value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func importNow(opts Options) time.Time {
	if opts.Now != nil {
		return startOfDay(opts.Now())
	}
	return startOfDay(time.Now())
}

func startOfDay(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func dateString(value time.Time) string {
	return startOfDay(value).Format(subscriptions.DateLayout)
}
