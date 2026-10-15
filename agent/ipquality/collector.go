package ipquality

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipidentity"
)

const (
	defaultLookupURL  = "https://api.ipapi.is"
	defaultServiceURL = ""
	defaultUserAgent  = "houfeng-agent/ip-quality"
	maxRawJSONBytes   = 128 * 1024
)

type Collector interface {
	Collect(context.Context, *agentapi.IPQualityPlan, time.Time) []agentapi.IPQualityReportPayload
}

type HTTPCollectorOptions struct {
	Client         *http.Client
	Client4        *http.Client
	Client6        *http.Client
	LocalAddresses func() ([]netip.Addr, error)
	LookupURL      string
	ServiceURL     string
	AgentVersion   string
	Fingerprint    string
	SyncBatchID    string
	UserAgent      string
}

type HTTPCollector struct {
	client         *http.Client
	serviceClient4 *http.Client
	serviceClient6 *http.Client
	localAddresses func() ([]netip.Addr, error)
	lookupURL      string
	serviceURL     string
	customLookup   bool
	agentVersion   string
	fingerprint    string
	syncBatchID    string
	userAgent      string
}

func NewHTTPCollector(opts HTTPCollectorOptions) *HTTPCollector {
	client := opts.Client
	lookupURL := strings.TrimSpace(opts.LookupURL)
	customLookup := lookupURL != ""
	serviceClient4, serviceClient6 := client, client
	if client == nil {
		if customLookup {
			client = http.DefaultClient
			serviceClient4, serviceClient6 = client, client
		} else {
			client = directHTTPClient("tcp")
			serviceClient4 = directHTTPClient("tcp4")
			serviceClient6 = directHTTPClient("tcp6")
		}
	}
	if opts.Client4 != nil {
		serviceClient4 = opts.Client4
	}
	if opts.Client6 != nil {
		serviceClient6 = opts.Client6
	}
	localAddresses := opts.LocalAddresses
	if localAddresses == nil {
		localAddresses = usableLocalAddresses
	}
	if lookupURL == "" {
		lookupURL = defaultLookupURL
	}
	serviceURL := strings.TrimSpace(opts.ServiceURL)
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &HTTPCollector{
		client:         client,
		serviceClient4: serviceClient4,
		serviceClient6: serviceClient6,
		localAddresses: localAddresses,
		lookupURL:      lookupURL,
		serviceURL:     serviceURL,
		customLookup:   customLookup,
		agentVersion:   opts.AgentVersion,
		fingerprint:    opts.Fingerprint,
		syncBatchID:    opts.SyncBatchID,
		userAgent:      userAgent,
	}
}

func directHTTPClient(network string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	}
	return &http.Client{Transport: transport}
}

func (c *HTTPCollector) serviceHTTPClient(ipVersion int) *http.Client {
	if ipVersion == 6 {
		return c.serviceClient6
	}
	return c.serviceClient4
}

func (c *HTTPCollector) WithMetadata(agentVersion, fingerprint, syncBatchID string) *HTTPCollector {
	cloned := *c
	cloned.agentVersion = agentVersion
	cloned.fingerprint = fingerprint
	cloned.syncBatchID = syncBatchID
	return &cloned
}

func (c *HTTPCollector) Collect(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) []agentapi.IPQualityReportPayload {
	var reports []agentapi.IPQualityReportPayload
	if !c.customLookup {
		reports = c.collectDefault(ctx, plan, observedAt)
	} else {
		reports = []agentapi.IPQualityReportPayload{c.collectLegacy(ctx, plan, observedAt)}
	}
	// An old Center strictly rejects unknown wire fields. Negotiate the entire
	// product, not just SMTP, before the result can reach the durable queue.
	if plan == nil || plan.ProductRevision != agentapi.IPQualityProductRevision || c.customLookup {
		for i := range reports {
			reports[i].Product = nil
			for j := range reports[i].ProviderResults {
				reports[i].ProviderResults[j].Details = nil
			}
			for j := range reports[i].ServiceUnlocks {
				reports[i].ServiceUnlocks[j].Checks = nil
			}
		}
	}
	return reports
}

func (c *HTTPCollector) collectLegacy(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) agentapi.IPQualityReportPayload {
	report := agentapi.IPQualityReportPayload{
		ObservedAt:   observedAt.UTC(),
		AgentVersion: c.agentVersion,
		Fingerprint:  c.fingerprint,
		SyncBatchID:  c.syncBatchID,
		IPAddress:    "0.0.0.0",
		IPVersion:    4,
		Status:       agentapi.IPQualityStatusFailure,
	}
	if plan == nil || !plan.Enabled {
		report.ErrorCode = "disabled"
		report.ErrorSummary = "ip quality collection disabled"
		return report
	}
	timeout := time.Duration(plan.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	collectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	lookupPayload, lookupRaw, err := c.getJSON(collectCtx, c.lookupURL)
	if err != nil {
		report.ErrorCode = "lookup_failed"
		report.ErrorSummary = err.Error()
		report.RawJSON = rawEnvelope(lookupRaw, nil)
		return report
	}
	reportedVersion := intFromMap(lookupPayload, "version", "ip_version")
	applyLookupPayload(&report, lookupPayload)
	parsedIP := net.ParseIP(report.IPAddress)
	if parsedIP == nil {
		report.IPAddress = "0.0.0.0"
		report.IPVersion = 4
	} else if reportedVersion != 4 && reportedVersion != 6 {
		if parsedIP.To4() == nil {
			report.IPVersion = 6
		} else {
			report.IPVersion = 4
		}
	}
	report.Status = agentapi.IPQualityStatusSuccess

	serviceRaw := make(map[string]json.RawMessage)
	if c.serviceURL != "" {
		for _, service := range normalizedServices(plan.Services) {
			unlock, raw, err := c.collectServiceUnlock(collectCtx, service)
			if len(raw) > 0 {
				serviceRaw[service] = raw
			}
			if err != nil {
				report.Status = agentapi.IPQualityStatusPartial
				unlock = agentapi.IPQualityServiceUnlockPayload{
					Service:      service,
					Status:       "unknown",
					ProbeStatus:  sourceStatusFailure,
					ErrorCode:    "probe_failed",
					ErrorSummary: err.Error(),
				}
			} else if unlock.ProbeStatus == sourceStatusFailure {
				report.Status = agentapi.IPQualityStatusPartial
			}
			report.ServiceUnlocks = append(report.ServiceUnlocks, unlock)
		}
	}
	report.RawJSON = rawEnvelope(lookupRaw, serviceRaw)
	return report
}

// usableLocalAddresses retains private IPv4 and ULA addresses: NAT is not
// evidence that a family is absent. Only UP non-loopback interfaces count.
func usableLocalAddresses() ([]netip.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addresses []netip.Addr
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		values, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			prefix, err := netip.ParsePrefix(value.String())
			if err == nil && prefix.Addr().IsGlobalUnicast() {
				addresses = append(addresses, prefix.Addr())
			}
		}
	}
	return addresses, nil
}

func familyPlaceholder(ipVersion int) string {
	if ipVersion == 6 {
		return "::"
	}
	return "0.0.0.0"
}

func (c *HTTPCollector) collectDefault(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) []agentapi.IPQualityReportPayload {
	startedAt := time.Now()
	collectionID := rand.Text()
	timeout := 60 * time.Second
	if plan != nil && plan.TimeoutSeconds > 0 {
		timeout = time.Duration(plan.TimeoutSeconds) * time.Second
	}
	collectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	families := agentapi.CollectionIPVersions(plan)
	reports := make([]agentapi.IPQualityReportPayload, len(families))
	if plan == nil || !plan.Enabled {
		for index, family := range families {
			reports[index] = agentapi.IPQualityReportPayload{
				ObservedAt: observedAt.UTC(), IPAddress: familyPlaceholder(family), IPVersion: family,
				AgentVersion: c.agentVersion, Fingerprint: c.fingerprint, SyncBatchID: c.syncBatchID,
				Status: agentapi.IPQualityStatusFailure, ErrorCode: "disabled", ErrorSummary: "ip quality collection disabled",
				DiagnosticsJSON: diagnosticsJSON(startedAt, nil, nil, family, agentapi.IPQualityFamilyUnknown, collectionID, false, nil, plan),
			}
		}
		return reports
	}
	addresses, addressErr := c.localAddresses()
	availability := [2]string{agentapi.IPQualityFamilyUnavailable, agentapi.IPQualityFamilyUnavailable}
	if addressErr != nil {
		availability = [2]string{agentapi.IPQualityFamilyUnknown, agentapi.IPQualityFamilyUnknown}
	} else {
		for _, address := range addresses {
			if !address.IsGlobalUnicast() {
				continue
			}
			index := 1
			if address.Is4() {
				index = 0
			}
			availability[index] = agentapi.IPQualityFamilyAvailable
		}
	}
	canonicalBudget, dependentBudget, _ := collectionBudgets(timeout)
	providerSlots, serviceSlots := make(chan struct{}, 4), make(chan struct{}, 7)
	mailSlots := make(chan struct{}, 6)
	canonicalSources, _ := defaultProviderSources()
	canonicalCtx, canonicalCancel := context.WithTimeout(collectCtx, canonicalBudget)
	var canonical [2][]providerSourceOutcome
	var workers sync.WaitGroup
	for _, family := range families {
		index := (family - 4) / 2
		if availability[index] == agentapi.IPQualityFamilyUnavailable {
			continue
		}
		workers.Add(1)
		go func(index, family int) {
			defer workers.Done()
			canonical[index] = c.collectProviderSources(canonicalCtx, canonicalSources, "", minDuration(canonicalBudget, 5*time.Second), providerSlots, family)
		}(index, family)
	}
	// A global barrier prevents dependent work from taking discovery slots.
	workers.Wait()
	canonicalCancel()
	for index, family := range families {
		workers.Add(1)
		go func(index, family int) {
			defer workers.Done()
			familyIndex := (family - 4) / 2
			reports[index] = c.collectDefaultFamily(collectCtx, plan, observedAt, startedAt, collectionID, family, availability[familyIndex], canonical[familyIndex], dependentBudget, providerSlots, serviceSlots, mailSlots)
		}(index, family)
	}
	workers.Wait()
	return reports
}

func (c *HTTPCollector) collectDefaultFamily(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time, startedAt time.Time, collectionID string, ipVersion int, availability string, canonicalOutcomes []providerSourceOutcome, dependentBudget time.Duration, providerSlots, serviceSlots, mailSlots chan struct{}) agentapi.IPQualityReportPayload {
	report := agentapi.IPQualityReportPayload{
		ObservedAt:   observedAt.UTC(),
		AgentVersion: c.agentVersion,
		Fingerprint:  c.fingerprint,
		SyncBatchID:  c.syncBatchID,
		IPAddress:    familyPlaceholder(ipVersion),
		IPVersion:    ipVersion,
		Status:       agentapi.IPQualityStatusFailure,
	}
	productEnabled := plan.ProductRevision == agentapi.IPQualityProductRevision
	mailEnabled := productEnabled && plan.MailEnabled != nil && *plan.MailEnabled
	if productEnabled {
		report.Product = &agentapi.IPQualityAgentProduct{Revision: 1, PTRNames: []string{}, Mail: []agentapi.IPQualityMailResult{}}
	}
	canonicalSources, targetDependentSources := defaultProviderSources()
	if availability == agentapi.IPQualityFamilyUnavailable {
		report.ErrorCode = "address_family_unavailable"
		report.ErrorSummary = "no usable local address for this IP family"
		for _, source := range append(canonicalSources, targetDependentSources...) {
			report.ProviderResults = append(report.ProviderResults, agentapi.IPQualityProviderResultPayload{
				Provider: source.Name(), Status: sourceStatusSkipped, SourceType: sourceTypeDefault,
				ErrorCode: report.ErrorCode, ErrorSummary: report.ErrorSummary,
			})
		}
		if mailEnabled {
			report.Product.Mail = skippedMailResults(observedAt, report.ErrorCode)
		}
		for _, service := range normalizedServices(plan.Services) {
			source, ok := defaultServiceSource(service)
			if !ok {
				report.ServiceUnlocks = append(report.ServiceUnlocks, unsupportedServiceOutcome(service).Result)
				continue
			}
			report.ServiceUnlocks = append(report.ServiceUnlocks, agentapi.IPQualityServiceUnlockPayload{
				Service: service, Source: source, Status: "unknown", ProbeStatus: sourceStatusSkipped,
				ErrorCode: report.ErrorCode, ErrorSummary: report.ErrorSummary,
			})
		}
		report.Coverage = coverageFromResults(report.ProviderResults, report.ServiceUnlocks)
		report.DiagnosticsJSON = diagnosticsJSON(startedAt, nil, nil, ipVersion, availability, collectionID, false, report.ProviderResults, plan)
		return report
	}
	providerOutcomes := append([]providerSourceOutcome(nil), canonicalOutcomes...)
	discoveryCandidates := map[string]string{}
	targetIP := ""
	// Registry order breaks ties within this family, never completion order.
	for _, outcome := range canonicalOutcomes {
		address, valid := ipidentity.Parse(outcome.IPAddress)
		if outcome.Result.Status == sourceStatusSuccess && valid && !address.IsUnspecified() {
			discoveryCandidates[outcome.Result.Provider] = outcome.IPAddress
			if targetIP == "" {
				targetIP = address.String()
			}
		}
	}
	if targetIP != "" {
		availability = agentapi.IPQualityFamilyAvailable
	}
	egressAmbiguous := discoveryEgressAmbiguous(canonicalOutcomes)
	var services []serviceProbeOutcome
	if targetIP != "" {
		// Only successful discovery rows for a different IP are queried again.
		// The three dependent providers plus at most one requery stay bounded.
		dependentSources := append([]providerSource(nil), targetDependentSources...)
		var requeryIndices []int
		for index, outcome := range canonicalOutcomes {
			if outcome.Result.Status == sourceStatusSuccess && ipidentity.Changed(outcome.IPAddress, targetIP) {
				dependentSources = append(dependentSources, canonicalSources[index])
				requeryIndices = append(requeryIndices, index)
			}
		}
		var dependentOutcomes []providerSourceOutcome
		var geoOutcomes []providerSourceOutcome
		var phases sync.WaitGroup
		phases.Add(2)
		go func() {
			defer phases.Done()
			dependentCtx, dependentCancel := context.WithTimeout(ctx, dependentBudget)
			defer dependentCancel()
			dependentOutcomes = c.collectProviderSources(dependentCtx, dependentSources, targetIP, minDuration(dependentBudget, 5*time.Second), providerSlots, ipVersion)
		}()
		go func() {
			defer phases.Done()
			requestedServices := normalizedServices(plan.Services)
			if egressAmbiguous {
				services = ambiguousServiceOutcomes(requestedServices, ipVersion)
				return
			}
			services = c.collectDefaultServiceUnlocksWithSemaphore(ctx, requestedServices, ipVersion, serviceSlots, targetIP)
		}()
		if !egressAmbiguous {
			phases.Add(1)
			go func() {
				defer phases.Done()
				geoOutcomes = collectScriptGeo(ctx, targetIP, ipVersion, observedAt, providerSlots)
			}()
		}
		if productEnabled {
			phases.Add(1)
			go func() {
				defer phases.Done()
				report.Product.PTRNames = collectPTR(ctx, targetIP)
			}()
		}
		if mailEnabled {
			phases.Add(1)
			go func() {
				defer phases.Done()
				if egressAmbiguous {
					report.Product.Mail = skippedMailResults(observedAt, "egress_ambiguous")
					return
				}
				report.Product.Mail = collectMail(ctx, targetIP, ipVersion, observedAt, mailSlots)
			}()
		}
		phases.Wait()
		for index, canonicalIndex := range requeryIndices {
			providerOutcomes[canonicalIndex] = dependentOutcomes[len(targetDependentSources)+index]
		}
		providerOutcomes = append(providerOutcomes, dependentOutcomes[:len(targetDependentSources)]...)
		providerOutcomes = append(providerOutcomes, geoOutcomes...)
	} else {
		if mailEnabled {
			report.Product.Mail = skippedMailResults(observedAt, "missing_target_ip")
		}
		for _, source := range targetDependentSources {
			providerOutcomes = append(providerOutcomes, sourceFailure(
				source.Name(),
				sourceTypeDefault,
				"missing_target_ip",
				"source requires a canonical IP from an earlier provider",
				nil,
				nil,
			))
		}
	}

	providerRaw := map[string]sourceRawEnvelope{}
	ipCandidates := map[string]string{}
	for _, outcome := range providerOutcomes {
		report.ProviderResults = append(report.ProviderResults, outcome.Result)
		providerRaw[outcome.Result.Provider] = sourceRawEnvelopeFromProvider(outcome)
		if outcome.Result.Status == sourceStatusSuccess && validIPAddress(outcome.IPAddress) {
			ipCandidates[outcome.Result.Provider] = outcome.IPAddress
		}
	}

	successfulProviders := successfulProviderOutcomes(providerOutcomes)
	report.Coverage = coverageFromResults(report.ProviderResults, nil)
	if len(successfulProviders) == 0 {
		report.ErrorCode = "lookup_failed"
		report.ErrorSummary = "all default IP quality provider sources failed"
		report.DiagnosticsJSON = diagnosticsJSON(startedAt, discoveryCandidates, ipCandidates, ipVersion, availability, collectionID, egressAmbiguous, report.ProviderResults, plan)
		report.RawJSON = rawEnvelopeV2(providerRaw, nil, report.DiagnosticsJSON)
		return report
	}

	report.IPAddress = targetIP
	report.IPVersion = ipVersion
	applyReportFallbacksFromProviders(&report, successfulProviders)

	serviceRaw := map[string]sourceRawEnvelope{}
	for _, outcome := range services {
		report.ServiceUnlocks = append(report.ServiceUnlocks, outcome.Result)
		serviceRaw[outcome.Result.Service] = sourceRawEnvelopeFromService(outcome)
	}
	report.Coverage = coverageFromResults(report.ProviderResults, report.ServiceUnlocks)
	report.DiagnosticsJSON = diagnosticsJSON(startedAt, discoveryCandidates, ipCandidates, ipVersion, availability, collectionID, egressAmbiguous, report.ProviderResults, plan)
	report.RawJSON = rawEnvelopeV2(providerRaw, serviceRaw, report.DiagnosticsJSON)
	report.Status = agentapi.IPQualityStatusSuccess
	if hasDefaultSourceFailure(report.ProviderResults) || hasServiceProbeFailure(report.ServiceUnlocks) || hasMailProbeFailure(report.Product) {
		report.Status = agentapi.IPQualityStatusPartial
	}
	return report
}

func discoveryEgressAmbiguous(outcomes []providerSourceOutcome) bool {
	for index, left := range outcomes {
		leftIP, leftOK := ipidentity.Parse(left.IPAddress)
		if left.Result.Status != sourceStatusSuccess || !leftOK {
			continue
		}
		for _, right := range outcomes[index+1:] {
			rightIP, rightOK := ipidentity.Parse(right.IPAddress)
			if right.Result.Status == sourceStatusSuccess && rightOK && leftIP.Is4() == rightIP.Is4() && leftIP != rightIP {
				return true
			}
		}
	}
	return false
}

func collectionBudgets(total time.Duration) (time.Duration, time.Duration, time.Duration) {
	if total <= 0 {
		return 0, 0, 0
	}
	stage := total / 3
	return stage, stage, total - stage - stage
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func (c *HTTPCollector) collectProviderSources(ctx context.Context, sources []providerSource, targetIP string, sourceTimeout time.Duration, slots chan struct{}, ipVersion int) []providerSourceOutcome {
	// Discovery, target-dependent lookups and canonical requeries must all
	// use the selected family's transport, not the generic legacy client.
	familyCollector := *c
	familyCollector.client = c.serviceHTTPClient(ipVersion)
	outcomes := make([]providerSourceOutcome, len(sources))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(sources))
	for index, source := range sources {
		index, source := index, source
		go func() {
			defer waitGroup.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				outcomes[index] = sourceFailure(source.Name(), sourceTypeDefault, errorCodeForHTTPError(ctx.Err()), "", nil, nil)
				return
			}
			if err := ctx.Err(); err != nil {
				outcomes[index] = sourceFailure(source.Name(), sourceTypeDefault, errorCodeForHTTPError(err), err.Error(), nil, nil)
				return
			}
			sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
			defer cancel()
			outcomes[index] = source.Collect(sourceCtx, &familyCollector, targetIP)
			if ipVersion != 0 && outcomes[index].Result.Status == sourceStatusSuccess {
				address, ok := ipidentity.Parse(outcomes[index].IPAddress)
				if !ok || address.IsUnspecified() || address.Is4() != (ipVersion == 4) {
					outcomes[index] = sourceFailure(source.Name(), sourceTypeDefault, "ip_mismatch", "", outcomes[index].Result.LatencyMS, outcomes[index].Raw)
					outcomes[index].Result.ErrorSummary = "provider returned a different IP"
				}
			}
		}()
	}
	waitGroup.Wait()
	return outcomes
}

func (c *HTTPCollector) collectServiceUnlock(ctx context.Context, service string) (agentapi.IPQualityServiceUnlockPayload, json.RawMessage, error) {
	url := strings.ReplaceAll(c.serviceURL, "{service}", service)
	payload, raw, err := c.getJSON(ctx, url)
	if err != nil {
		return agentapi.IPQualityServiceUnlockPayload{}, raw, err
	}
	result := agentapi.IPQualityServiceUnlockPayload{
		Service:      service,
		Status:       stringFromMap(payload, "status", "unlock_status"),
		Region:       stringFromMap(payload, "region", "unlock_region", "country", "country_code"),
		UnlockType:   stringFromMap(payload, "unlock_type", "type"),
		ErrorCode:    stringFromMap(payload, "error_code"),
		ErrorSummary: stringFromMap(payload, "error_summary", "message"),
	}
	status := strings.ToLower(strings.TrimSpace(result.Status))
	if status == "" {
		if unlocked := boolFromMap(payload, "unlocked"); unlocked != nil && *unlocked {
			status = "unlocked"
		} else if unlocked != nil {
			status = "blocked"
		} else {
			status = "unknown"
		}
	}
	switch status {
	case "unlocked", "blocked", "partial":
		result.Status = status
		result.ProbeStatus = sourceStatusSuccess
	default:
		result.Status = "unknown"
		result.ProbeStatus = sourceStatusFailure
		result.Region = ""
		result.UnlockType = ""
		result.ErrorCode = "invalid_response"
		result.ErrorSummary = "service response did not establish a business conclusion"
	}
	return result, raw, nil
}

func (c *HTTPCollector) getJSON(ctx context.Context, url string) (map[string]any, json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRawJSONBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxRawJSONBytes {
		body = body[:maxRawJSONBytes]
	}
	if !looksLikeJSONObject(body) {
		return nil, nil, &sourceHTTPError{code: "non_json_response", status: response.StatusCode}
	}
	raw := json.RawMessage(append([]byte(nil), body...))
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, &sourceHTTPError{code: "non_json_response", status: response.StatusCode, cause: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, raw, &sourceHTTPError{code: "http_status", status: response.StatusCode}
	}
	return payload, raw, nil
}

func looksLikeJSONObject(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func applyLookupPayload(report *agentapi.IPQualityReportPayload, payload map[string]any) {
	if version := intFromMap(payload, "version", "ip_version"); version == 4 || version == 6 {
		report.IPVersion = version
	}
	if hasIPAPIShape(payload) {
		facts := ipapiISFactsFromPayload(payload)
		report.IPAddress = firstNonEmpty(facts.ip, stringFromMap(payload, "query"))
		report.ASN = facts.asn
		report.Organization = facts.organization
		report.Latitude = facts.latitude
		report.Longitude = facts.longitude
		report.UseRegionCode = facts.regionCode
		report.UseRegionName = facts.regionName
		report.RegisteredRegionCode = stringFromMap(payload, "registered_region_code", "registered_country_code")
		report.RegisteredRegionName = firstNonEmpty(
			stringFromMap(payload, "registered_region_name", "registered_country_name"),
		)
		report.RiskLevel = stringFromMap(payload, "risk_level", "risk")
		report.ProviderResults = providerResultsFromLookup(payload)
		return
	}
	report.IPAddress = stringFromMap(payload, "ip", "ip_address", "query")
	report.IPVersion = intFromMap(payload, "version", "ip_version")
	report.ASN = asnStringFromPayload(payload)
	report.Organization = firstNonEmpty(
		stringFromMap(payload, "organization", "org", "isp"),
		stringFromNestedMap(payload, "asn", "org", "organization", "name"),
		stringFromNestedMap(payload, "company", "name"),
	)
	report.Latitude = floatPtrFromMap(payload, "latitude", "lat")
	if report.Latitude == nil {
		report.Latitude = floatPtrFromNestedMap(payload, "location", "latitude", "lat")
	}
	report.Longitude = floatPtrFromMap(payload, "longitude", "lon", "lng")
	if report.Longitude == nil {
		report.Longitude = floatPtrFromNestedMap(payload, "location", "longitude", "lon", "lng")
	}
	report.UseRegionCode = firstNonEmpty(
		stringFromMap(payload, "use_region_code", "country_code", "country"),
		stringFromNestedMap(payload, "location", "country_code"),
	)
	report.UseRegionName = firstNonEmpty(
		stringFromMap(payload, "use_region_name", "country_name"),
		stringFromNestedMap(payload, "location", "country"),
	)
	report.RegisteredRegionCode = stringFromMap(payload, "registered_region_code", "registered_country_code")
	report.RegisteredRegionName = stringFromMap(payload, "registered_region_name", "registered_country_name")
	report.RiskLevel = stringFromMap(payload, "risk_level", "risk")
	report.ProviderResults = providerResultsFromLookup(payload)
}

func providerResultsFromLookup(payload map[string]any) []agentapi.IPQualityProviderResultPayload {
	values, ok := payload["provider_results"].([]any)
	if !ok {
		values, ok = payload["providers"].([]any)
	}
	if ok {
		out := make([]agentapi.IPQualityProviderResultPayload, 0, len(values))
		for _, value := range values {
			providerMap, ok := value.(map[string]any)
			if !ok {
				continue
			}
			result := providerResultFromMap(providerMap)
			if result.Provider != "" {
				out = append(out, result)
			}
		}
		return out
	}
	result := providerResultFromMap(payload)
	if result.Provider == "" {
		if hasIPAPIShape(payload) {
			result.Provider = "ipapi.is"
		} else {
			result.Provider = "lookup"
		}
	}
	return []agentapi.IPQualityProviderResultPayload{result}
}

func providerResultFromMap(payload map[string]any) agentapi.IPQualityProviderResultPayload {
	return agentapi.IPQualityProviderResultPayload{
		Provider:     stringFromMap(payload, "provider", "source"),
		UsageType:    firstNonEmpty(stringFromMap(payload, "usage_type", "type"), stringFromNestedMap(payload, "asn", "type")),
		CompanyType:  firstNonEmpty(stringFromMap(payload, "company_type", "company"), stringFromNestedMap(payload, "company", "type")),
		RiskLevel:    stringFromMap(payload, "risk_level", "risk"),
		RiskScore:    stringFromMap(payload, "risk_score", "score"),
		RegionCode:   firstNonEmpty(stringFromMap(payload, "region_code", "country_code", "country"), stringFromNestedMap(payload, "location", "country_code")),
		RegionName:   firstNonEmpty(stringFromMap(payload, "region_name", "country_name"), stringFromNestedMap(payload, "location", "country")),
		IsProxy:      boolFromMap(payload, "is_proxy", "proxy"),
		IsTor:        boolFromMap(payload, "is_tor", "tor"),
		IsVPN:        boolFromMap(payload, "is_vpn", "vpn"),
		IsServer:     boolFromMap(payload, "is_server", "is_datacenter", "server", "hosting", "datacenter"),
		IsAbuser:     boolFromMap(payload, "is_abuser", "abuser", "abuse"),
		IsRobot:      boolFromMap(payload, "is_robot", "is_crawler", "robot", "bot", "crawler"),
		ErrorCode:    stringFromMap(payload, "error_code"),
		ErrorSummary: stringFromMap(payload, "error_summary", "message"),
	}
}

func hasIPAPIShape(payload map[string]any) bool {
	for _, key := range []string{
		"asn",
		"company",
		"location",
		"is_datacenter",
		"is_proxy",
		"is_vpn",
		"is_tor",
		"is_abuser",
		"is_crawler",
	} {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

func asnStringFromPayload(payload map[string]any) string {
	asn, _ := ipapiASNFromPayload(payload)
	if asn != "" {
		return asn
	}
	value := firstNonEmpty(
		stringFromMap(payload, "asn", "as", "as_number"),
		stringFromNestedMap(payload, "asn", "asn", "as", "as_number"),
	)
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(strings.TrimSpace(value))
	if strings.HasPrefix(upper, "AS") {
		return upper
	}
	return "AS" + value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nestedMap(values map[string]any, key string) map[string]any {
	child, ok := values[key].(map[string]any)
	if !ok {
		return nil
	}
	return child
}

func stringFromNestedMap(values map[string]any, key string, nestedKeys ...string) string {
	child := nestedMap(values, key)
	if child == nil {
		return ""
	}
	return stringFromMap(child, nestedKeys...)
}

func floatPtrFromNestedMap(values map[string]any, key string, nestedKeys ...string) *float64 {
	child := nestedMap(values, key)
	if child == nil {
		return nil
	}
	return floatPtrFromMap(child, nestedKeys...)
}

func rawEnvelope(lookup json.RawMessage, services map[string]json.RawMessage) json.RawMessage {
	envelope := map[string]any{}
	if len(lookup) > 0 {
		var lookupValue any
		if json.Unmarshal(lookup, &lookupValue) == nil {
			envelope["lookup"] = sanitizeRawJSONValue(lookupValue)
		}
	}
	if len(services) > 0 {
		serviceValues := map[string]any{}
		for service, raw := range services {
			var value any
			if json.Unmarshal(raw, &value) == nil {
				serviceValues[service] = sanitizeRawJSONValue(value)
			}
		}
		if len(serviceValues) > 0 {
			envelope["services"] = serviceValues
		}
	}
	payload, err := json.Marshal(envelope)
	if err != nil || len(payload) == 0 {
		return nil
	}
	if len(payload) > maxRawJSONBytes {
		envelope["truncated"] = true
		delete(envelope, "services")
		payload, err = json.Marshal(envelope)
		if err != nil || len(payload) == 0 {
			return nil
		}
		if len(payload) > maxRawJSONBytes {
			return nil
		}
	}
	return json.RawMessage(payload)
}

func sanitizeRawJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveRawJSONKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = sanitizeRawJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, sanitizeRawJSONValue(child))
		}
		return out
	default:
		return value
	}
}

func isSensitiveRawJSONKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	return normalized == "authorization" ||
		normalized == "cookie" ||
		normalized == "set_cookie" ||
		normalized == "token" ||
		normalized == "access_token" ||
		normalized == "refresh_token" ||
		normalized == "api_key" ||
		normalized == "apikey" ||
		normalized == "secret" ||
		strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_key") ||
		strings.Contains(normalized, "password")
}

func normalizedServices(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		service := strings.ToLower(strings.TrimSpace(value))
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		out = append(out, service)
	}
	return out
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case json.Number:
			return typed.String()
		case float64:
			return fmt.Sprintf("%.0f", typed)
		case bool:
			if typed {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func intFromMap(values map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if n, err := typed.Int64(); err == nil {
				return int(n)
			}
		case float64:
			return int(typed)
		case string:
			if strings.Contains(typed, "6") {
				return 6
			}
			if strings.Contains(typed, "4") {
				return 4
			}
		}
	}
	return 0
}

func floatPtrFromMap(values map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if n, err := typed.Float64(); err == nil {
				return &n
			}
		case float64:
			n := typed
			return &n
		}
	}
	return nil
}

func boolFromMap(values map[string]any, keys ...string) *bool {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return &typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "yes", "1":
				v := true
				return &v
			case "false", "no", "0":
				v := false
				return &v
			}
		}
	}
	return nil
}
