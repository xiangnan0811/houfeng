package ipquality

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

// IntelligenceCollector performs network I/O only. The repository commits its
// deferred cache writes and snapshot together after checking ownership/settings.
type IntelligenceCollector struct {
	repo IntelligenceRepository
	http *http.Client
	dns  *intelligenceDNS
}

func NewIntelligenceCollector(repo IntelligenceRepository) *IntelligenceCollector {
	return &IntelligenceCollector{repo: repo, http: newIntelligenceHTTPClient(), dns: newIntelligenceDNS()}
}

type intelligenceSession struct {
	collector *IntelligenceCollector
	repo      IntelligenceRepository
	ctx       context.Context
	cancel    context.CancelFunc
	claim     IntelligenceClaim
	slots     chan struct{}
	mu        sync.Mutex
	snapshot  IntelligenceSnapshot
	inactive  bool
	quotas    map[string]time.Time
}

func credentialFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (c *IntelligenceCollector) Collect(ctx context.Context, claim IntelligenceClaim, config settings.IPQualitySettings) IntelligenceSnapshot {
	parent := ctx
	deadline := time.Now().Add(60 * time.Second)
	if !claim.DeadlineAt.IsZero() && claim.DeadlineAt.Before(deadline) {
		deadline = claim.DeadlineAt
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	s := &intelligenceSession{
		collector: c, repo: c.repo, ctx: ctx, cancel: cancel, claim: claim, slots: make(chan struct{}, 4), quotas: map[string]time.Time{},
		snapshot: IntelligenceSnapshot{
			Providers: []ProviderResultRead{}, Blacklists: []agentapi.IPQualityBlacklistResult{},
			ASN:            agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
			Registration:   agentapi.IPQualityRegistration{Status: "unknown"},
			SourceOutcomes: []SourceOutcome{}, CredentialFingerprints: map[string]string{},
		},
	}
	if config.Sources.IPAPIISKey != "" {
		s.snapshot.CredentialFingerprints["ipapi.is"] = credentialFingerprint(config.Sources.IPAPIISKey)
	}
	if config.Sources.AbuseIPDBKey != "" {
		s.snapshot.CredentialFingerprints["abuseipdb"] = credentialFingerprint(config.Sources.AbuseIPDBKey)
	}
	if fp := settings.MaxMindCredentialFingerprint(config.Sources.MaxMindAccountID, config.Sources.MaxMindLicenseKey); fp != "" {
		s.snapshot.CredentialFingerprints["maxmind"] = fp
	}
	addr, err := netip.ParseAddr(claim.Report.IPAddress)
	if !config.Enabled || err != nil || !publicIntelligenceAddress(addr) {
		s.snapshot.State = "skipped"
		return s.snapshot
	}
	if err = s.eligible(); err != nil {
		if !errors.Is(err, errIntelligenceInactive) {
			now := time.Now().UTC()
			s.snapshot.CompletedAt = &now
			s.snapshot.State = "failed"
			return s.snapshot
		}
		s.snapshot.State = "skipped"
		return s.snapshot
	}
	var wg sync.WaitGroup
	launch := func(work func()) {
		wg.Add(1)
		go func() { defer wg.Done(); work() }()
	}
	launch(s.collectRIS)
	launch(s.collectRegistration)
	launch(func() { s.collectDNSBL("spamcop", addr) })
	launch(func() { s.collectDNSBL("dronebl", addr) })
	launch(func() { s.collectDROP(addr) })
	if config.Sources.IPAPIISKey != "" {
		launch(func() { s.collectProvider("ipapi.is", config.Sources.IPAPIISKey) })
	}
	if config.Sources.AbuseIPDBKey != "" {
		launch(func() { s.collectProvider("abuseipdb", config.Sources.AbuseIPDBKey) })
	}
	if config.Sources.MaxMindAccountID != "" && config.Sources.MaxMindLicenseKey != "" {
		launch(func() { s.collectMaxMind(config.Sources.MaxMindAccountID, config.Sources.MaxMindLicenseKey) })
	}
	wg.Wait()
	if s.snapshot.ASN.Number != "" && ctx.Err() == nil {
		s.collectASNRegistration()
		// Public fields are immutable after this point. Key revocation restores
		// this complete value instead of guessing which merged fields were keyed.
		publicASN := s.snapshot.ASN
		publicASN.Origins = append([]string{}, publicASN.Origins...)
		s.snapshot.PublicASN = &publicASN
		if config.Sources.IPAPIISKey != "" {
			s.collectKeyedASN(config.Sources.IPAPIISKey)
		}
	}
	return s.finish(parent)
}

// A repository error is not proof the asset was revoked. Cancel further
// requests, but keep facts already collected. Only a definitive false result
// marks the session inactive.
func (s *intelligenceSession) eligible() error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if s.repo == nil {
		return nil
	}
	ok, err := s.repo.IPQualityIntelligenceEligible(s.ctx, s.claim)
	if err != nil {
		if s.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if s.ctx.Err() != nil {
				return s.ctx.Err()
			}
			return err
		}
		s.cancelRequests()
		return intelligenceError("eligibility_failed")
	}
	if !ok {
		s.markInactive()
		return errIntelligenceInactive
	}
	return nil
}

func (s *intelligenceSession) cancelRequests() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *intelligenceSession) markInactive() {
	s.mu.Lock()
	s.inactive = true
	s.mu.Unlock()
	s.cancelRequests()
}

// finish rechecks eligibility on a context that outlives the collection budget.
// Cancellation and deadline are not lifecycle revocation; only a definitive
// false result skips and drops deferred cache writes.
func (s *intelligenceSession) finish(parent context.Context) IntelligenceSnapshot {
	now := time.Now().UTC()
	s.snapshot.CompletedAt = &now
	if s.inactive || errors.Is(s.authoritativeEligibility(parent), errIntelligenceInactive) {
		s.snapshot.State = "skipped"
		s.snapshot.CacheWrites = nil
		return s.snapshot
	}
	success, failure := 0, 0
	for _, outcome := range s.snapshot.SourceOutcomes {
		if outcome.Status == "success" {
			success++
		}
		if outcome.Status == "failure" {
			failure++
		}
	}
	switch {
	case success == 0:
		s.snapshot.State = "failed"
	case failure > 0:
		s.snapshot.State = "partial"
	default:
		s.snapshot.State = "complete"
	}
	sort.Slice(s.snapshot.Providers, func(i, j int) bool { return s.snapshot.Providers[i].Provider < s.snapshot.Providers[j].Provider })
	sort.Slice(s.snapshot.Blacklists, func(i, j int) bool { return s.snapshot.Blacklists[i].Source < s.snapshot.Blacklists[j].Source })
	sort.Slice(s.snapshot.SourceOutcomes, func(i, j int) bool {
		return s.snapshot.SourceOutcomes[i].SourceID < s.snapshot.SourceOutcomes[j].SourceID
	})
	if err := ValidateIntelligenceSnapshot(s.snapshot, s.claim.Report.IPAddress); err != nil {
		s.snapshot.Providers = []ProviderResultRead{}
		s.snapshot.Blacklists = []agentapi.IPQualityBlacklistResult{}
		s.snapshot.Registration = agentapi.IPQualityRegistration{Status: "unknown"}
		s.snapshot.ASN = agentapi.IPQualityASN{Status: "unknown", Origins: []string{}}
		s.snapshot.PublicASN = nil
		s.snapshot.CacheWrites = nil
		s.snapshot.State = "failed"
		s.snapshot.SourceOutcomes = []SourceOutcome{{SourceID: "ripe-ris", Status: "failure", ErrorCode: "invalid_snapshot", ObservedAt: now}}
	}
	return s.snapshot
}

func (s *intelligenceSession) authoritativeEligibility(parent context.Context) error {
	if s.repo == nil {
		return nil
	}
	ctx := parent
	if ctx == nil || ctx.Err() != nil {
		ctx = s.ctx
	}
	if ctx == nil || ctx.Err() != nil {
		if ctx != nil {
			return ctx.Err()
		}
		return intelligenceError("eligibility_failed")
	}
	ok, err := s.repo.IPQualityIntelligenceEligible(ctx, s.claim)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		return intelligenceError("eligibility_failed")
	}
	if !ok {
		s.markInactive()
		return errIntelligenceInactive
	}
	return nil
}

func (s *intelligenceSession) outcome(source string, err error) {
	status, code := "success", ""
	if err != nil {
		status = "failure"
		code = safeIntelligenceError(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, outcome := range s.snapshot.SourceOutcomes {
		if outcome.SourceID == source {
			if status == "failure" {
				s.snapshot.SourceOutcomes[i] = SourceOutcome{source, status, code, time.Now().UTC()}
			}
			return
		}
	}
	s.snapshot.SourceOutcomes = append(s.snapshot.SourceOutcomes, SourceOutcome{source, status, code, time.Now().UTC()})
}

func safeIntelligenceError(err error) string {
	var known intelligenceError
	if errors.As(err, &known) {
		return string(known)
	}
	if errors.Is(err, errIntelligenceInactive) {
		return "lifecycle_inactive"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	return "invalid_response"
}

func (s *intelligenceSession) addCache(entry SourceCacheEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.CacheWrites = append(s.snapshot.CacheWrites, entry)
	if entry.BlockedUntil != nil {
		s.quotas[entry.SourceID+":"+entry.CredentialFingerprint] = *entry.BlockedUntil
	}
}

func (s *intelligenceSession) load(source, resource, fp string, out any) bool {
	if s.repo == nil {
		return false
	}
	entry, err := s.repo.LoadIPQualitySourceCache(s.ctx, source, resource, fp)
	if err != nil || entry == nil || !entry.ExpiresAt.After(time.Now()) || ValidateIntelligenceCacheEntry(*entry) != nil {
		return false
	}
	return json.Unmarshal(entry.PayloadJSON, out) == nil
}

func (s *intelligenceSession) cache(source, resource, fp string, value any, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	entry := SourceCacheEntry{SourceID: source, Resource: resource, CredentialFingerprint: fp, FetchedAt: now, ExpiresAt: now.Add(ttl), PayloadJSON: data}
	if ValidateIntelligenceCacheEntry(entry) == nil {
		s.addCache(entry)
	}
}

func ValidateIntelligenceSnapshot(snapshot IntelligenceSnapshot, target string) error {
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > 512<<10 {
		return errors.New("snapshot size")
	}
	if len(snapshot.Providers) > 16 || len(snapshot.Blacklists) > 8 || len(snapshot.SourceOutcomes) > 16 {
		return errors.New("snapshot rows")
	}
	if err := validateIntelligenceText(reflect.ValueOf(snapshot)); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, row := range snapshot.Providers {
		if seen[row.Provider] {
			return errors.New("duplicate provider")
		}
		seen[row.Provider] = true
		if err := validateCenterProvider(row, target); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	for _, row := range snapshot.Blacklists {
		if seen[row.Source] {
			return errors.New("duplicate blacklist")
		}
		seen[row.Source] = true
		category, method := "spam", "dnsbl"
		switch row.Source {
		case "spamcop":
		case "dronebl":
			category = "abuse"
		case "spamhaus-drop":
			category = "network"
			method = "dataset"
		default:
			return errors.New("blacklist source")
		}
		if row.Category != category || row.QueryMethod != method || len(row.ErrorCode) > 64 || len(row.Codes) > 32 || row.ObservedAt.IsZero() {
			return errors.New("blacklist fields")
		}
		switch row.Status {
		case "listed", "not_listed", "unknown", "unsupported":
		default:
			return errors.New("blacklist status")
		}
		if row.Status == "listed" && len(row.Codes) == 0 {
			return errors.New("blacklist codes")
		}
		if row.Status != "listed" && (len(row.Codes) > 0 || row.MatchedRange != "") {
			return errors.New("failed blacklist facts")
		}
		addr, addrErr := netip.ParseAddr(target)
		if addrErr != nil {
			return errors.New("blacklist target")
		}
		if method == "dnsbl" {
			if row.Status == "unsupported" && addr.Is4() || (row.Status == "listed" || row.Status == "not_listed") && !addr.Is4() {
				return errors.New("DNSBL family")
			}
			for _, code := range row.Codes {
				answer, err := netip.ParseAddr(code)
				if err != nil || !answer.Is4() {
					return errors.New("DNSBL code")
				}
				octets := answer.As4()
				class := int(octets[3])
				known := row.Source == "spamcop" && class == 2 || row.Source == "dronebl" && (class == 2 || class == 3 || class >= 5 && class <= 18 || class == 255)
				if octets[0] != 127 || octets[1] != 0 || octets[2] != 0 || !known {
					return errors.New("DNSBL code")
				}
			}
		} else if row.Status == "listed" || row.Status == "not_listed" {
			if row.DatasetAt == nil || row.DatasetAt.IsZero() || row.Status == "listed" && row.MatchedRange == "" {
				return errors.New("DROP dataset facts")
			}
		}
		if row.ReferenceURL != "" {
			u, e := url.Parse(row.ReferenceURL)
			if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return errors.New("blacklist URL")
			}
			allowed := row.Source == "spamcop" && u.Host == "www.spamcop.net" || row.Source == "dronebl" && u.Host == "dronebl.org" || row.Source == "spamhaus-drop" && u.Host == "www.spamhaus.org"
			if !allowed {
				return errors.New("blacklist URL source")
			}
		}
		if row.MatchedRange != "" {
			prefix, e := netip.ParsePrefix(row.MatchedRange)
			addr, e2 := netip.ParseAddr(target)
			if e != nil || e2 != nil || prefix.Addr().Is4() != addr.Is4() || !prefix.Contains(addr) {
				return errors.New("blacklist range")
			}
		}
	}
	seen = map[string]bool{}
	for _, row := range snapshot.SourceOutcomes {
		if !validIntelligenceSource(row.SourceID) || seen[row.SourceID] || row.Status != "success" && row.Status != "failure" && row.Status != "skipped" || len(row.ErrorCode) > 64 || row.ObservedAt.IsZero() {
			return errors.New("source outcome")
		}
		seen[row.SourceID] = true
	}
	if err := validateRegistration(snapshot.Registration, target); err != nil {
		return err
	}
	return validateASN(snapshot.ASN, target)
}

func validateCenterProvider(row ProviderResultRead, target string) error {
	if row.Provider != "ipapi.is" && row.Provider != "abuseipdb" && row.Provider != "maxmind" {
		return errors.New("snapshot provider")
	}
	if row.Details == nil || row.Details.Method != "center_api" || row.Details.APITier != "authenticated" || row.RiskLevel != "" || len(row.ErrorCode) > 64 {
		return errors.New("snapshot provider facts")
	}
	if row.Status == "failure" {
		if row.ErrorCode == "" || len(row.ExtraJSON) != 0 || len(row.Details.Scores) != 0 || len(row.Details.FieldStates) != 0 {
			return errors.New("failed provider diagnostics")
		}
		for _, char := range row.ErrorCode {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
				return errors.New("failed provider error code")
			}
		}
		facts := row
		facts.Provider, facts.Status, facts.SourceType = "", "", ""
		facts.ErrorCode, facts.ErrorSummary = "", ""
		facts.LatencyMS, facts.Details, facts.ExtraJSON = nil, nil, nil
		if !reflect.DeepEqual(facts, ProviderResultRead{}) {
			return errors.New("failed provider carrier facts")
		}
		details := *row.Details
		details.QueriedIP, details.Method, details.APITier = "", "", ""
		details.ObservedAt = time.Time{}
		details.Scores, details.FieldStates = nil, nil
		if !reflect.DeepEqual(details, agentapi.IPQualityProviderDetails{}) {
			return errors.New("failed provider detail facts")
		}
		details = *row.Details
		details.Method, details.APITier = "agent_public", "anonymous"
		return agentapi.ValidateIPQualityProviderDetails(row.Provider, &details, 1, target)
	}
	if row.Status != "success" {
		return errors.New("snapshot provider status")
	}
	if len(row.ExtraJSON) > 0 {
		if row.Provider != "abuseipdb" || len(row.ExtraJSON) > 1024 || !json.Valid(row.ExtraJSON) {
			return errors.New("provider extra")
		}
		var extra struct {
			TotalReports   *int64     `json:"total_reports"`
			LastReportedAt *time.Time `json:"last_reported_at"`
		}
		decoder := json.NewDecoder(bytes.NewReader(row.ExtraJSON))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&extra); err != nil {
			return errors.New("provider extra schema")
		}
		if extra.TotalReports != nil && (*extra.TotalReports < 0 || *extra.TotalReports > 9007199254740991) {
			return errors.New("provider report count")
		}
	}
	for _, fact := range []struct {
		key   string
		value *bool
	}{
		{"is_proxy", row.IsProxy}, {"is_vpn", row.IsVPN}, {"is_tor", row.IsTor},
		{"is_server", row.IsServer}, {"is_abuser", row.IsAbuser}, {"is_robot", row.IsRobot},
	} {
		if fact.value != nil && row.Details.FieldStates[fact.key] != "known" {
			return errors.New("unknown provider fact")
		}
		if fact.value == nil && row.Details.FieldStates[fact.key] == "known" {
			return errors.New("missing provider fact")
		}
	}
	// Reuse the complete typed-field validator, without relaxing the Agent wire
	// boundary. The copied metadata is only a validation view, not persisted.
	details := *row.Details
	details.Method = "agent_public"
	details.APITier = "anonymous"
	return agentapi.ValidateIPQualityProviderDetails(row.Provider, &details, 1, target)
}

func validCountryCode(value string) bool {
	return value == "" || len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
}
func validFactStatus(value string) bool {
	return value == "" || value == "unknown" || value == "success"
}
func validateSourceURL(value string, rdap bool) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || !validIntelligenceURL(u) || u.Fragment != "" {
		return errors.New("source URL")
	}
	if rdap && rirHosts[u.Hostname()] == "" {
		return errors.New("RIR source URL")
	}
	if !rdap && u.Hostname() != "stat.ripe.net" {
		return errors.New("RIS source URL")
	}
	return nil
}
func validateRegistration(value agentapi.IPQualityRegistration, target string) error {
	if !validFactStatus(value.Status) || !validCountryCode(value.NetworkCountryCode) || !validCountryCode(value.RegistrantCountryCode) {
		return errors.New("registration status or country")
	}
	if err := validateSourceURL(value.SourceURL, true); err != nil {
		return err
	}
	if value.Status != "success" {
		if value.NetworkCountryCode != "" || value.RegistrantCountryCode != "" || value.StartAddress != "" || value.EndAddress != "" || value.NetworkName != "" || value.RegistrantName != "" {
			return errors.New("unknown registration facts")
		}
		return nil
	}
	addr, e0 := netip.ParseAddr(target)
	start, e1 := netip.ParseAddr(value.StartAddress)
	end, e2 := netip.ParseAddr(value.EndAddress)
	if e0 != nil || e1 != nil || e2 != nil || start.Is4() != addr.Is4() || end.Is4() != addr.Is4() || start.Compare(addr) > 0 || end.Compare(addr) < 0 {
		return errors.New("registration range")
	}
	return nil
}
func validateASN(value agentapi.IPQualityASN, target string) error {
	if !validFactStatus(value.Status) || !validCountryCode(value.CountryCode) || len(value.Origins) > 64 {
		return errors.New("ASN fields")
	}
	if value.Status != "success" && (value.Number != "" || value.Name != "" || value.Domain != "" || value.Type != "" || value.CountryCode != "" || value.RIR != "" || value.Route != "" || len(value.Origins) > 0 || value.IPv4Prefixes != nil || value.IPv4Addresses != nil || value.IPv6Prefixes != nil || value.IPv6_48s != nil) {
		return errors.New("unknown ASN facts")
	}
	if value.Number != "" && asNumber(value.Number) != value.Number {
		return errors.New("ASN number")
	}
	for _, origin := range value.Origins {
		if asNumber(origin) != origin {
			return errors.New("ASN origin")
		}
	}
	for _, at := range []*time.Time{value.RegisteredAt, value.UpdatedAt, value.ObservedAt} {
		if at != nil && at.IsZero() {
			return errors.New("ASN timestamp")
		}
	}
	for _, size := range []*string{value.IPv4Prefixes, value.IPv4Addresses, value.IPv6Prefixes, value.IPv6_48s} {
		if size != nil && !decimalString(*size) {
			return errors.New("ASN size")
		}
	}
	if value.Route != "" {
		prefix, e := netip.ParsePrefix(value.Route)
		addr, e2 := netip.ParseAddr(target)
		if e != nil || e2 != nil || prefix.Addr().Is4() != addr.Is4() || !prefix.Contains(addr) {
			return errors.New("ASN route")
		}
	}
	if value.SourceURL != "" {
		u, err := url.Parse(value.SourceURL)
		if err != nil || !validIntelligenceURL(u) || u.Fragment != "" || u.Hostname() != "stat.ripe.net" && rirHosts[u.Hostname()] == "" {
			return errors.New("ASN source URL")
		}
	}
	if value.Domain != "" {
		details := agentapi.IPQualityProviderDetails{QueriedIP: target, ObservedAt: time.Now().UTC(), Method: "agent_public", APITier: "anonymous", ASDomain: value.Domain}
		if err := agentapi.ValidateIPQualityProviderDetails("ipapi.is", &details, 1, target); err != nil {
			return err
		}
	}
	return nil
}

func validateIntelligenceText(v reflect.Value) error {
	if !v.IsValid() {
		return nil
	}
	if v.Type() == reflect.TypeOf(time.Time{}) || v.Type() == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return validateIntelligenceText(v.Elem())
		}
	case reflect.String:
		if len(v.String()) > 256 || !utf8.ValidString(v.String()) || strings.ContainsAny(v.String(), "\x00\r\n") {
			return errors.New("oversized or unsafe text")
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).Tag.Get("json") == "-" {
				continue
			}
			if err := validateIntelligenceText(v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if err := validateIntelligenceText(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			if err := validateIntelligenceText(key); err != nil {
				return err
			}
			if err := validateIntelligenceText(v.MapIndex(key)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validIntelligenceSource(source string) bool {
	switch source {
	case "ipapi.is", "abuseipdb", "maxmind", "ripe-ris", "rdap-ip", "rdap-asn", "spamcop", "dronebl", "spamhaus-drop":
		return true
	}
	return false
}

// ValidateIntelligenceCacheEntry rejects raw HTTP objects. Only the normalized
// schemas below are legal; successful data and @quota never share a resource.
func ValidateIntelligenceCacheEntry(entry SourceCacheEntry) error {
	if !validIntelligenceSource(entry.SourceID) || len(entry.PayloadJSON) > 8<<20 || !json.Valid(entry.PayloadJSON) {
		return errors.New("cache source or size")
	}
	authenticated := entry.SourceID == "ipapi.is" || entry.SourceID == "abuseipdb" || entry.SourceID == "maxmind"
	if authenticated {
		decoded, err := hex.DecodeString(entry.CredentialFingerprint)
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("cache namespace")
		}
	} else if entry.CredentialFingerprint != "" {
		return errors.New("public cache namespace")
	}
	if entry.Resource == "@quota" {
		if entry.BlockedUntil == nil || strings.TrimSpace(string(entry.PayloadJSON)) != "{}" {
			return errors.New("quota payload")
		}
		return nil
	}
	if entry.BlockedUntil != nil {
		return errors.New("quota attached to successful data")
	}
	var value any
	switch {
	case entry.SourceID == "spamhaus-drop":
		value = &dropDataset{}
	case strings.HasPrefix(entry.Resource, "bootstrap:") && (entry.SourceID == "rdap-ip" || entry.SourceID == "rdap-asn"):
		value = &bootstrapData{}
	case entry.SourceID == "rdap-ip":
		value = &agentapi.IPQualityRegistration{}
	case entry.SourceID == "rdap-asn" || entry.SourceID == "ripe-ris" || entry.SourceID == "ipapi.is" && strings.HasPrefix(entry.Resource, "AS"):
		value = &agentapi.IPQualityASN{}
	case authenticated:
		value = &ProviderResultRead{}
	default:
		return errors.New("unsupported cache")
	}
	decoder := json.NewDecoder(bytes.NewReader(entry.PayloadJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("cache schema: %w", err)
	}
	if dataset, ok := value.(*dropDataset); ok {
		if entry.Resource != "ipv4" && entry.Resource != "ipv6" || entry.Resource == "ipv4" && dataset.Family != 4 || entry.Resource == "ipv6" && dataset.Family != 6 {
			return errors.New("DROP cache family")
		}
		return validateDROP(*dataset)
	}
	if err := validateIntelligenceText(reflect.ValueOf(value)); err != nil {
		return err
	}
	switch parsed := value.(type) {
	case *bootstrapData:
		kind := strings.TrimPrefix(entry.Resource, "bootstrap:")
		if kind != "ipv4" && kind != "ipv6" && kind != "asn" || len(parsed.Entries) == 0 {
			return errors.New("bootstrap cache")
		}
		for _, item := range parsed.Entries {
			u, err := url.Parse(item.BaseURL)
			if err != nil || !validIntelligenceURL(u) || rirHosts[u.Hostname()] == "" || u.RawQuery != "" || u.Fragment != "" {
				return errors.New("bootstrap URL")
			}
			if kind == "asn" {
				if !validASNInterval(item.Resource) {
					return errors.New("bootstrap ASN range")
				}
			} else {
				prefix, err := netip.ParsePrefix(item.Resource)
				if err != nil || prefix.Addr().Is4() != (kind == "ipv4") {
					return errors.New("bootstrap prefix")
				}
			}
		}
	case *ProviderResultRead:
		if parsed.Provider != entry.SourceID {
			return errors.New("cached provider source")
		}
		if parsed.Status != "success" {
			return errors.New("cached provider status")
		}
		return validateCenterProvider(*parsed, entry.Resource)
	case *agentapi.IPQualityRegistration:
		if parsed.Status != "success" {
			return errors.New("cached registration status")
		}
		return validateRegistration(*parsed, entry.Resource)
	case *agentapi.IPQualityASN:
		if parsed.Status != "success" {
			return errors.New("cached ASN status")
		}
		target := entry.Resource
		if strings.HasPrefix(target, "AS") {
			if parsed.Number != target {
				return errors.New("cached ASN identity")
			}
			target = "8.8.8.8" // ASN caches contain no IP route; only domain validation needs an IP.
			if parsed.Route != "" {
				return errors.New("ASN cache contains IP route")
			}
		}
		return validateASN(*parsed, target)
	}
	return nil
}
