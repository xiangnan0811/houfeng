package ipquality

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipqualitydata"
)

func decodeIntelligenceJSON(data []byte) (map[string]any, error) {
	if !json.Valid(data) {
		return nil, intelligenceError("invalid_json")
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return nil, intelligenceError("invalid_json")
	}
	return payload, nil
}
func object(value any) map[string]any { result, _ := value.(map[string]any); return result }
func text(value any) string           { result, _ := value.(string); return strings.TrimSpace(result) }
func country(value any) string {
	result := strings.ToUpper(text(value))
	if len(result) == 2 && result[0] >= 'A' && result[0] <= 'Z' && result[1] >= 'A' && result[1] <= 'Z' {
		return result
	}
	return ""
}
func decimalString(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func decimal(value any) *string {
	n, ok := value.(json.Number)
	if !ok || !decimalString(string(n)) {
		return nil
	}
	v := string(n)
	return &v
}
func asNumber(value any) string {
	var raw string
	switch v := value.(type) {
	case string:
		raw = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(v)), "AS")
	case json.Number:
		raw = string(v)
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || n == 0 {
		return ""
	}
	return "AS" + strconv.FormatUint(n, 10)
}
func timestamp(value any) *time.Time {
	raw := text(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05"} {
		if at, err := time.Parse(layout, raw); err == nil {
			at = at.UTC()
			return &at
		}
	}
	return nil
}

func (s *intelligenceSession) collectProvider(source, key string) {
	fp, target := credentialFingerprint(key), s.claim.Report.IPAddress
	var row ProviderResultRead
	if s.load(source, target, fp, &row) {
		s.mu.Lock()
		s.snapshot.Providers = append(s.snapshot.Providers, row)
		s.mu.Unlock()
		s.outcome(source, nil)
		return
	}
	endpoint, method := "https://api.ipapi.is/", http.MethodPost
	body, _ := json.Marshal(map[string]string{"q": target, "key": key})
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if source == "abuseipdb" {
		endpoint = "https://api.abuseipdb.com/api/v2/check?ipAddress=" + url.QueryEscape(target) + "&maxAgeInDays=90"
		method = http.MethodGet
		body = nil
		headers = http.Header{"Key": []string{key}}
	}
	data, err := s.request(source, target, fp, method, endpoint, bytes.NewReader(body), headers, 2<<20)
	if err == nil {
		var payload map[string]any
		payload, err = decodeIntelligenceJSON(data)
		if err == nil {
			var parsed agentapi.IPQualityProviderResultPayload
			parsed, err = ipqualitydata.ParseProvider(source, payload, target, time.Now().UTC(), "center_api", "authenticated")
			if err == nil {
				row = providerRead(parsed)
				err = ValidateIntelligenceSnapshot(IntelligenceSnapshot{Providers: []ProviderResultRead{row}}, target)
				if err == nil && credentialEcho(reflect.ValueOf(row), key) {
					err = intelligenceError("unsafe_response")
				}
			}
		}
	}
	if err == nil {
		s.mu.Lock()
		s.snapshot.Providers = append(s.snapshot.Providers, row)
		s.mu.Unlock()
		s.cache(source, target, fp, row, time.Hour)
	} else {
		row = ProviderResultRead{
			Provider: source, Status: "failure", SourceType: "default", ErrorCode: safeIntelligenceError(err),
			Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: target, ObservedAt: time.Now().UTC(), Method: "center_api", APITier: "authenticated",
				Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{},
			},
		}
		s.mu.Lock()
		s.snapshot.Providers = append(s.snapshot.Providers, row)
		s.mu.Unlock()
	}
	s.outcome(source, err)
}
func providerRead(p agentapi.IPQualityProviderResultPayload) ProviderResultRead {
	return ProviderResultRead{
		Provider: p.Provider, Status: p.Status, SourceType: p.SourceType, UsageType: p.UsageType, CompanyType: p.CompanyType,
		RegionCode: p.RegionCode, RegionName: p.RegionName, IsProxy: p.IsProxy, IsTor: p.IsTor, IsVPN: p.IsVPN,
		IsServer: p.IsServer, IsAbuser: p.IsAbuser, IsRobot: p.IsRobot, Details: p.Details, ExtraJSON: p.ExtraJSON,
	}
}

func parseRISNetwork(data []byte, target, knownASN string) (agentapi.IPQualityASN, error) {
	result := agentapi.IPQualityASN{Status: "unknown", Origins: []string{}}
	payload, err := decodeIntelligenceJSON(data)
	if err != nil {
		return result, err
	}
	d := object(payload["data"])
	prefix, err := netip.ParsePrefix(text(d["prefix"]))
	addr, addrErr := netip.ParseAddr(target)
	if err != nil || addrErr != nil || !prefix.Contains(addr) || prefix.Addr().Is4() != addr.Is4() {
		return result, intelligenceError("invalid_prefix")
	}
	raw, ok := d["asns"].([]any)
	if !ok || len(raw) > 64 {
		return result, intelligenceError("invalid_origins")
	}
	seen := map[string]bool{}
	for _, v := range raw {
		asn := asNumber(v)
		if asn == "" {
			return result, intelligenceError("invalid_origins")
		}
		if !seen[asn] {
			result.Origins = append(result.Origins, asn)
			seen[asn] = true
		}
	}
	sort.Strings(result.Origins)
	result.Number = selectRISPrimary(result.Origins, knownASN)
	result.Status = "success"
	result.Route = prefix.String()
	result.SourceURL = "https://stat.ripe.net/data/network-info/data.json?resource=" + url.QueryEscape(target)
	now := time.Now().UTC()
	result.ObservedAt = &now
	return result, nil
}
func parseRISRouting(data []byte, asn *agentapi.IPQualityASN) error {
	payload, err := decodeIntelligenceJSON(data)
	if err != nil {
		return err
	}
	d := object(payload["data"])
	space := object(d["announced_space"])
	v4, v6 := object(space["v4"]), object(space["v6"])
	asn.IPv4Prefixes = decimal(v4["prefixes"])
	asn.IPv4Addresses = decimal(v4["ips"])
	asn.IPv6Prefixes = decimal(v6["prefixes"])
	asn.IPv6_48s = decimal(v6["48s"])
	if at := timestamp(d["query_time"]); at != nil {
		asn.ObservedAt = at
	}
	if asn.IPv4Prefixes == nil && asn.IPv4Addresses == nil && asn.IPv6Prefixes == nil && asn.IPv6_48s == nil {
		return intelligenceError("invalid_routing_status")
	}
	return nil
}
func selectRISPrimary(origins []string, knownASN string) string {
	seen := map[string]bool{}
	for _, origin := range origins {
		if origin != "" {
			seen[origin] = true
		}
	}
	if known := asNumber(knownASN); seen[known] {
		return known
	}
	if len(origins) == 1 && origins[0] != "" {
		return origins[0]
	}
	return ""
}

func retainRISRoute(asn agentapi.IPQualityASN) agentapi.IPQualityASN {
	return agentapi.IPQualityASN{
		Status:    asn.Status,
		SourceURL: asn.SourceURL,
		Route:     asn.Route,
		Origins:   append([]string{}, asn.Origins...),
	}
}

func (s *intelligenceSession) collectRIS() {
	source, target := "ripe-ris", s.claim.Report.IPAddress
	var asn agentapi.IPQualityASN
	if s.load(source, target, "", &asn) {
		s.finishCachedRIS(source, target, asn)
		return
	}
	data, err := s.request(source, target, "", http.MethodGet, "https://stat.ripe.net/data/network-info/data.json?resource="+url.QueryEscape(target), nil, nil, 2<<20)
	if err != nil {
		s.outcome(source, err)
		return
	}
	asn, err = parseRISNetwork(data, target, s.claim.Report.ASN)
	if err != nil {
		s.outcome(source, err)
		return
	}
	if asn.Number != "" {
		if primaryErr := s.fetchRISPrimary(&asn); primaryErr != nil {
			err = primaryErr
		}
	}
	s.storeRIS(source, target, asn, err, true)
}

func (s *intelligenceSession) finishCachedRIS(source, target string, cached agentapi.IPQualityASN) {
	selected := selectRISPrimary(cached.Origins, s.claim.Report.ASN)
	if selected != "" && selected == cached.Number {
		s.mu.Lock()
		s.snapshot.ASN = cached
		s.mu.Unlock()
		s.outcome(source, nil)
		return
	}
	asn := retainRISRoute(cached)
	now := time.Now().UTC()
	asn.ObservedAt = &now
	var err error
	if selected != "" {
		asn.Number = selected
		err = s.fetchRISPrimary(&asn)
	}
	// The retained route/origins were not refetched. Never renew their IP-cache TTL.
	s.storeRIS(source, target, asn, err, false)
}

func (s *intelligenceSession) fetchRISPrimary(asn *agentapi.IPQualityASN) error {
	source := "ripe-ris"
	var err error
	data, overviewErr := s.request(source, asn.Number, "", http.MethodGet, "https://stat.ripe.net/data/as-overview/data.json?resource="+asn.Number, nil, nil, 2<<20)
	if overviewErr == nil {
		var payload map[string]any
		payload, overviewErr = decodeIntelligenceJSON(data)
		if overviewErr == nil {
			d := object(payload["data"])
			if announced, ok := d["announced"].(bool); !ok || !announced {
				overviewErr = intelligenceError("invalid_as_overview")
			} else {
				asn.Name = text(d["holder"])
			}
		}
	}
	if overviewErr != nil {
		err = overviewErr
	}
	data, routingErr := s.request(source, asn.Number, "", http.MethodGet, "https://stat.ripe.net/data/routing-status/data.json?resource="+asn.Number, nil, nil, 2<<20)
	if routingErr == nil {
		routingErr = parseRISRouting(data, asn)
	}
	if routingErr != nil {
		err = routingErr
	}
	return err
}

func (s *intelligenceSession) storeRIS(source, target string, asn agentapi.IPQualityASN, err error, allowCache bool) {
	if textErr := validateIntelligenceText(reflect.ValueOf(asn)); textErr != nil {
		s.outcome(source, textErr)
		return
	}
	s.mu.Lock()
	s.snapshot.ASN = asn
	s.mu.Unlock()
	if err == nil && allowCache {
		s.cache(source, target, "", asn, 8*time.Hour)
	}
	s.outcome(source, err)
}

type bootstrapEntry struct {
	Resource string `json:"resource"`
	BaseURL  string `json:"base_url"`
}
type bootstrapData struct {
	Entries []bootstrapEntry `json:"entries"`
}

func validASNInterval(raw string) bool {
	bounds := strings.Split(raw, "-")
	if len(bounds) != 2 || asNumber(bounds[0]) == "" || asNumber(bounds[1]) == "" {
		return false
	}
	lo, _ := strconv.ParseUint(bounds[0], 10, 32)
	hi, _ := strconv.ParseUint(bounds[1], 10, 32)
	return lo <= hi
}
func parseBootstrap(data []byte, kind string) (bootstrapData, error) {
	result := bootstrapData{Entries: []bootstrapEntry{}}
	payload, err := decodeIntelligenceJSON(data)
	if err != nil {
		return result, err
	}
	services, ok := payload["services"].([]any)
	if !ok {
		return result, intelligenceError("invalid_bootstrap")
	}
	for _, raw := range services {
		pair, ok := raw.([]any)
		if !ok || len(pair) != 2 {
			return result, intelligenceError("invalid_bootstrap")
		}
		resources, ok := pair[0].([]any)
		if !ok {
			return result, intelligenceError("invalid_bootstrap")
		}
		urls, ok := pair[1].([]any)
		if !ok {
			return result, intelligenceError("invalid_bootstrap")
		}
		base := ""
		for _, rawURL := range urls {
			u, err := url.Parse(text(rawURL))
			if err == nil && validIntelligenceURL(u) && rirHosts[u.Hostname()] != "" && u.RawQuery == "" && u.Fragment == "" {
				base = strings.TrimRight(u.String(), "/")
				break
			}
		}
		if base == "" {
			continue
		}
		for _, rawResource := range resources {
			resource := text(rawResource)
			if kind == "asn" {
				if !validASNInterval(resource) {
					return result, intelligenceError("invalid_bootstrap")
				}
			} else {
				prefix, err := netip.ParsePrefix(resource)
				if err != nil || prefix.Addr().Is4() != (kind == "ipv4") {
					return result, intelligenceError("invalid_bootstrap")
				}
			}
			result.Entries = append(result.Entries, bootstrapEntry{resource, base})
		}
	}
	if len(result.Entries) == 0 {
		return result, intelligenceError("bootstrap_no_service")
	}
	return result, nil
}
func (b bootstrapData) endpoint(resource string, asn bool) (string, error) {
	best, base := -1, ""
	for _, entry := range b.Entries {
		if asn {
			bounds := strings.Split(entry.Resource, "-")
			if len(bounds) != 2 {
				continue
			}
			lo, e1 := strconv.ParseUint(bounds[0], 10, 32)
			hi, e2 := strconv.ParseUint(bounds[1], 10, 32)
			n, e3 := strconv.ParseUint(strings.TrimPrefix(resource, "AS"), 10, 32)
			if e1 == nil && e2 == nil && e3 == nil && n >= lo && n <= hi {
				base = entry.BaseURL
				break
			}
		} else {
			prefix, e1 := netip.ParsePrefix(entry.Resource)
			addr, e2 := netip.ParseAddr(resource)
			if e1 == nil && e2 == nil && prefix.Contains(addr) && prefix.Bits() > best {
				base = entry.BaseURL
				best = prefix.Bits()
			}
		}
	}
	if base == "" {
		return "", intelligenceError("bootstrap_no_service")
	}
	if asn {
		return base + "/autnum/" + strings.TrimPrefix(resource, "AS"), nil
	}
	return base + "/ip/" + resource, nil
}
func (s *intelligenceSession) rdapEndpoint(source, resource, kind string) (string, error) {
	var bootstrap bootstrapData
	if !s.load(source, "bootstrap:"+kind, "", &bootstrap) {
		data, err := s.request(source, "bootstrap:"+kind, "", http.MethodGet, "https://data.iana.org/rdap/"+kind+".json", nil, nil, 8<<20)
		if err != nil {
			return "", err
		}
		bootstrap, err = parseBootstrap(data, kind)
		if err != nil {
			return "", err
		}
		s.cache(source, "bootstrap:"+kind, "", bootstrap, 24*time.Hour)
	}
	return bootstrap.endpoint(resource, kind == "asn")
}

func rdapEvents(payload map[string]any) (*time.Time, *time.Time) {
	var registered, updated *time.Time
	events, _ := payload["events"].([]any)
	for _, v := range events {
		event := object(v)
		switch text(event["eventAction"]) {
		case "registration":
			registered = timestamp(event["eventDate"])
		case "last changed":
			updated = timestamp(event["eventDate"])
		}
	}
	return registered, updated
}
func rdapRegistrant(payload map[string]any) (string, string) {
	entities, _ := payload["entities"].([]any)
	for _, v := range entities {
		entity := object(v)
		roles, _ := entity["roles"].([]any)
		registrant := false
		for _, role := range roles {
			if text(role) == "registrant" {
				registrant = true
			}
		}
		if !registrant {
			continue
		}
		card, _ := entity["vcardArray"].([]any)
		if len(card) != 2 {
			continue
		}
		props, _ := card[1].([]any)
		name, code := "", ""
		for _, v := range props {
			property, _ := v.([]any)
			if len(property) != 4 {
				continue
			}
			switch text(property[0]) {
			case "fn":
				name = text(property[3])
			case "adr":
				address, _ := property[3].([]any)
				if len(address) == 7 {
					code = country(address[6])
				}
				// RFC 8605 cc is explicit structured country evidence. Never parse
				// a label/free-form street address, administrative contact or link.
				if explicit := country(object(property[1])["cc"]); explicit != "" {
					code = explicit
				}
			}
		}
		return name, code
	}
	return "", ""
}
func parseRDAPNetwork(data []byte, target, endpoint string) (agentapi.IPQualityRegistration, error) {
	result := agentapi.IPQualityRegistration{Status: "unknown"}
	p, err := decodeIntelligenceJSON(data)
	if err != nil {
		return result, err
	}
	if text(p["objectClassName"]) != "ip network" {
		return result, intelligenceError("invalid_rdap_network")
	}
	start, e1 := netip.ParseAddr(text(p["startAddress"]))
	end, e2 := netip.ParseAddr(text(p["endAddress"]))
	addr, e3 := netip.ParseAddr(target)
	if e1 != nil || e2 != nil || e3 != nil || start.Is4() != addr.Is4() || end.Is4() != addr.Is4() || start.Compare(addr) > 0 || end.Compare(addr) < 0 {
		return result, intelligenceError("invalid_rdap_range")
	}
	u, _ := url.Parse(endpoint)
	result = agentapi.IPQualityRegistration{
		Status: "success", RIR: rirHosts[u.Hostname()], NetworkHandle: text(p["handle"]),
		StartAddress: start.String(), EndAddress: end.String(), NetworkName: text(p["name"]),
		NetworkCountryCode: country(p["country"]), SourceURL: endpoint,
	}
	result.RegisteredAt, result.UpdatedAt = rdapEvents(p)
	result.RegistrantName, result.RegistrantCountryCode = rdapRegistrant(p)
	if err := validateIntelligenceText(reflect.ValueOf(result)); err != nil {
		return agentapi.IPQualityRegistration{Status: "unknown"}, err
	}
	return result, nil
}
func (s *intelligenceSession) collectRegistration() {
	source, target := "rdap-ip", s.claim.Report.IPAddress
	var result agentapi.IPQualityRegistration
	if s.load(source, target, "", &result) {
		s.mu.Lock()
		s.snapshot.Registration = result
		s.mu.Unlock()
		s.outcome(source, nil)
		return
	}
	kind := "ipv6"
	if s.claim.Report.IPVersion == 4 {
		kind = "ipv4"
	}
	endpoint, err := s.rdapEndpoint(source, target, kind)
	if err == nil {
		var data []byte
		data, err = s.request(source, target, "", http.MethodGet, endpoint, nil, nil, 2<<20)
		if err == nil {
			result, err = parseRDAPNetwork(data, target, endpoint)
		}
	}
	if err == nil {
		s.mu.Lock()
		s.snapshot.Registration = result
		s.mu.Unlock()
		s.cache(source, target, "", result, 24*time.Hour)
	}
	s.outcome(source, err)
}
func parseRDAPASN(data []byte, number, endpoint string) (agentapi.IPQualityASN, error) {
	result := agentapi.IPQualityASN{Status: "unknown", Origins: []string{}}
	p, err := decodeIntelligenceJSON(data)
	if err != nil {
		return result, err
	}
	if text(p["objectClassName"]) != "autnum" || asNumber(p["startAutnum"]) == "" || asNumber(p["endAutnum"]) == "" {
		return result, intelligenceError("invalid_rdap_autnum")
	}
	n, _ := strconv.ParseUint(strings.TrimPrefix(number, "AS"), 10, 32)
	lo, _ := strconv.ParseUint(strings.TrimPrefix(asNumber(p["startAutnum"]), "AS"), 10, 32)
	hi, _ := strconv.ParseUint(strings.TrimPrefix(asNumber(p["endAutnum"]), "AS"), 10, 32)
	if n < lo || n > hi {
		return result, intelligenceError("invalid_rdap_autnum")
	}
	u, _ := url.Parse(endpoint)
	result.Status = "success"
	result.Number = number
	result.Name = text(p["name"])
	result.CountryCode = country(p["country"])
	result.RIR = rirHosts[u.Hostname()]
	result.SourceURL = endpoint
	result.RegisteredAt, result.UpdatedAt = rdapEvents(p)
	return result, validateIntelligenceText(reflect.ValueOf(result))
}
func (s *intelligenceSession) collectASNRegistration() {
	source, number := "rdap-asn", s.snapshot.ASN.Number
	var result agentapi.IPQualityASN
	var err error
	if !s.load(source, number, "", &result) {
		var endpoint string
		endpoint, err = s.rdapEndpoint(source, number, "asn")
		if err == nil {
			var data []byte
			data, err = s.request(source, number, "", http.MethodGet, endpoint, nil, nil, 2<<20)
			if err == nil {
				result, err = parseRDAPASN(data, number, endpoint)
			}
		}
		if err == nil {
			s.cache(source, number, "", result, 24*time.Hour)
		}
	}
	if err == nil {
		s.mu.Lock()
		s.snapshot.ASN.RIR = result.RIR
		s.snapshot.ASN.CountryCode = result.CountryCode
		s.snapshot.ASN.RegisteredAt = result.RegisteredAt
		s.snapshot.ASN.UpdatedAt = result.UpdatedAt
		if s.snapshot.ASN.Name == "" {
			s.snapshot.ASN.Name = result.Name
		}
		s.mu.Unlock()
	}
	s.outcome(source, err)
}
func (s *intelligenceSession) collectKeyedASN(key string) {
	source, number := "ipapi.is", s.snapshot.ASN.Number
	fp := credentialFingerprint(key)
	var result agentapi.IPQualityASN
	var err error
	if !s.load(source, number, fp, &result) {
		body, _ := json.Marshal(map[string]string{"q": number, "key": key})
		var data []byte
		data, err = s.request(source, number, fp, http.MethodPost, "https://api.ipapi.is/", bytes.NewReader(body), http.Header{"Content-Type": []string{"application/json"}}, 2<<20)
		if err == nil {
			result, err = parseKeyedASN(data, number)
		}
		if err == nil && credentialEcho(reflect.ValueOf(result), key) {
			err = intelligenceError("unsafe_response")
		}
		if err == nil {
			s.cache(source, number, fp, result, time.Hour)
		}
	}
	if err == nil {
		s.mu.Lock()
		s.snapshot.ASN.Domain = result.Domain
		s.snapshot.ASN.Type = result.Type
		if result.Name != "" {
			s.snapshot.ASN.Name = result.Name
		}
		if result.RIR != "" {
			s.snapshot.ASN.RIR = result.RIR
		}
		if result.RegisteredAt != nil {
			s.snapshot.ASN.RegisteredAt = result.RegisteredAt
		}
		if result.UpdatedAt != nil {
			s.snapshot.ASN.UpdatedAt = result.UpdatedAt
		}
		s.mu.Unlock()
	}
	s.outcome(source, err)
}
func parseKeyedASN(data []byte, number string) (agentapi.IPQualityASN, error) {
	p, err := decodeIntelligenceJSON(data)
	result := agentapi.IPQualityASN{Status: "unknown", Origins: []string{}}
	if err != nil {
		return result, err
	}
	if nested := object(p[number]); nested != nil {
		p = nested
	}
	if nested := object(p["asn"]); nested != nil {
		p = nested
	}
	actual := asNumber(p["asn"])
	if actual == "" {
		actual = asNumber(p["number"])
	}
	if actual != number {
		return result, intelligenceError("target_mismatch")
	}
	result.Status = "success"
	result.Number = number
	result.Name = text(p["org"])
	result.Domain = text(p["domain"])
	result.Type = text(p["type"])
	result.RIR = text(p["rir"])
	result.RegisteredAt = timestamp(p["created"])
	result.UpdatedAt = timestamp(p["updated"])
	if result.Domain == "" && result.Type == "" && result.RIR == "" {
		return result, errors.New("missing asn fields")
	}
	if err := validateIntelligenceText(reflect.ValueOf(result)); err != nil {
		return result, err
	}
	return result, validateASN(result, "8.8.8.8")
}

// Even a credential echoed inside an otherwise whitelisted business string
// is not a fact we may retain. Never serialize this response in diagnostics.
func credentialEcho(value reflect.Value, key string) bool {
	if key == "" || !value.IsValid() || value.Type() == reflect.TypeOf(time.Time{}) {
		return false
	}
	switch value.Kind() {
	case reflect.String:
		return strings.Contains(value.String(), key)
	case reflect.Pointer, reflect.Interface:
		return !value.IsNil() && credentialEcho(value.Elem(), key)
	case reflect.Struct:
		for i := range value.NumField() {
			if credentialEcho(value.Field(i), key) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			if credentialEcho(value.Index(i), key) {
				return true
			}
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			if credentialEcho(iter.Value(), key) {
				return true
			}
		}
	}
	return false
}
