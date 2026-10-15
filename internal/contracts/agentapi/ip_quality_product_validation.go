package agentapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"unicode/utf8"

	"houfeng/internal/ipidentity"
)

const (
	ipQualityProductStringMax        = 256
	ipQualityProductErrorCodeMax     = 64
	ipQualityProductScoreMax         = 8
	ipQualityProductProviderMax      = 16
	ipQualityProductMailMax          = 12
	ipQualityProductChecksMax        = 4
	ipQualityProductPTRMax           = 4
	ipQualityProductPTRBytesMax      = 1024
	ipQualityAgentProductBytesMax    = 64 << 10
	ipQualityProviderDetailsBytesMax = 8 << 10
	ipQualityServiceChecksBytesMax   = 16 << 10
)

// ErrInvalidIPQualityProduct is returned when typed product fields are unsafe.
// Callers must reject the report. Validation never truncates or rewrites facts.
var ErrInvalidIPQualityProduct = errors.New("invalid ip quality product")

// Legacy agent providers remain acceptable. Revision 1 does not apply the new
// 16-row cap unless a product is present, and it does not drop these IDs.
var ipQualityProviderRegistry = map[string]struct{}{
	"ipapi.is":       {},
	"ipquery.io":     {},
	"proxycheck.io":  {},
	"ip2location.io": {},
	"ipwho.is":       {},
	"maxmind":        {},
	"ipinfo":         {},
	"ipregistry":     {},
	"ipdata":         {},
	"ipqualityscore": {},
	"abuseipdb":      {},
	"scamalytics":    {},
	"ipapi.co":       {},
	"ip-api.com":     {},
	"db-ip":          {},
	"ippure":         {},
	"meowvps":        {},
	"ip.net.coffee":  {},
}

var ipQualityMailDomains = map[string]string{
	"gmail":   "gmail.com",
	"outlook": "outlook.com",
	"yahoo":   "yahoo.com",
	"apple":   "me.com",
	"mailru":  "mail.ru",
	"aol":     "aol.com",
	"gmx":     "gmx.com",
	"mailcom": "mail.com",
	"163":     "163.com",
	"sohu":    "sohu.com",
	"sina":    "sina.com",
	"qq":      "qq.com",
}

var ipQualityServiceChecks = map[string]map[string]struct{}{
	"youtube-premium":    {"premium": {}},
	"amazon-prime-video": {"home": {}},
	"chatgpt":            {"web": {}, "app": {}},
	"netflix":            {"title_81280792": {}, "title_70143836": {}},
	"disney-plus":        {"region": {}},
	"tiktok":             {"web_region": {}},
	"reddit":             {"web": {}},
}

// Field-state keys are the six existing risk booleans, usage/company type, and
// the typed location/network fields. is_abuser is the existing wire name.
var ipQualityFieldStateKeys = map[string]struct{}{
	"is_proxy": {}, "is_vpn": {}, "is_tor": {}, "is_abuser": {}, "is_robot": {}, "is_server": {},
	"usage_type": {}, "company_type": {},
	"country_code": {}, "region": {}, "city": {}, "postal_code": {}, "timezone": {},
	"latitude": {}, "longitude": {}, "registered_country_code": {},
	"asn": {}, "as_name": {}, "as_domain": {}, "as_country_code": {}, "route": {},
	"company_name": {}, "company_domain": {},
	"is_mobile": {}, "is_anycast": {}, "is_bogon": {},
}

// ValidateIPQualityProduct checks an agent wire report. A nil product keeps the
// legacy carrier, including all 18 registered providers, and rejects details or
// checks. Direct writes should call the three helpers below on the existing
// carrier instead of allocating a wire copy.
func ValidateIPQualityProduct(report IPQualityReportPayload) error {
	if err := ValidateIPQualityAgentProduct(report.Product, report.IPAddress, report.IPVersion, len(report.ProviderResults)); err != nil {
		return err
	}
	revision := 0
	if report.Product != nil {
		revision = report.Product.Revision
	}
	for _, provider := range report.ProviderResults {
		if err := ValidateIPQualityProviderDetails(provider.Provider, provider.Details, revision, report.IPAddress); err != nil {
			return err
		}
	}
	for _, unlock := range report.ServiceUnlocks {
		if err := ValidateIPQualityServiceChecks(unlock.Service, unlock.Checks, revision); err != nil {
			return err
		}
	}
	return nil
}

// ValidateIPQualityAgentProduct validates the agent product, mail registry, and
// the revision-1 provider cap. A nil product is legacy and ignores providerCount.
// Callers must still validate every provider and service row.
func ValidateIPQualityAgentProduct(product *IPQualityAgentProduct, ip string, family int, providerCount int) error {
	if product == nil {
		return nil
	}
	if product.Revision != IPQualityProductRevision {
		return invalidProduct("product revision")
	}
	if providerCount < 0 || providerCount > ipQualityProductProviderMax {
		return invalidProduct("provider count")
	}
	addr, ok := ipidentity.Parse(ip)
	if !ok || (family != 4 && family != 6) || addressFamily(addr) != family {
		return invalidProduct("report address")
	}
	if len(product.PTRNames) > ipQualityProductPTRMax {
		return invalidProduct("ptr count")
	}
	seenPTR := make(map[string]struct{}, len(product.PTRNames))
	ptrBytes := 0
	for _, name := range product.PTRNames {
		if _, isIP := ipidentity.Parse(name); !validHostname(name) || isIP {
			return invalidProduct("ptr name")
		}
		key := strings.ToLower(strings.TrimSuffix(name, "."))
		if _, ok := seenPTR[key]; ok {
			return invalidProduct("duplicate ptr")
		}
		seenPTR[key] = struct{}{}
		ptrBytes += len(name)
	}
	if ptrBytes > ipQualityProductPTRBytesMax {
		return invalidProduct("ptr size")
	}
	if len(product.Mail) > ipQualityProductMailMax {
		return invalidProduct("mail count")
	}
	seenMail := make(map[string]struct{}, len(product.Mail))
	for _, row := range product.Mail {
		domain, ok := ipQualityMailDomains[row.Provider]
		if !ok || row.Domain != domain {
			return invalidProduct("mail provider")
		}
		if _, ok := seenMail[row.Provider]; ok {
			return invalidProduct("duplicate mail provider")
		}
		seenMail[row.Provider] = struct{}{}
		if !validMailStatus(row.Status) || row.ObservedAt.IsZero() || !validOptionalToken(row.ErrorCode, ipQualityProductErrorCodeMax) {
			return invalidProduct("mail status")
		}
		if row.MXHost != "" && !validHostname(row.MXHost) {
			return invalidProduct("mx host")
		}
		if row.TargetIP != "" {
			target, ok := ipidentity.Parse(row.TargetIP)
			if !ok || addressFamily(target) != family || !publicAddress(target) {
				return invalidProduct("mail target")
			}
		}
		if row.GreetingCode != nil && (*row.GreetingCode < 100 || *row.GreetingCode > 599) {
			return invalidProduct("greeting code")
		}
		if row.LatencyMS != nil && *row.LatencyMS < 0 {
			return invalidProduct("mail latency")
		}
	}
	if !withinJSONBytes(product, ipQualityAgentProductBytesMax) {
		return invalidProduct("product size")
	}
	return nil
}

// ValidateIPQualityProviderDetails validates one provider row. Revision 1 checks
// the provider registry even when details is nil, so callers must invoke it for
// every row. Details are rejected unless productRevision is 1. Agent reports
// cannot claim center_api or authenticated results.
func ValidateIPQualityProviderDetails(provider string, details *IPQualityProviderDetails, productRevision int, ip string) error {
	if productRevision != IPQualityProductRevision {
		if details != nil {
			return invalidProduct("details without product revision")
		}
		return nil
	}
	if _, ok := ipQualityProviderRegistry[provider]; !ok {
		return invalidProduct("provider")
	}
	if details == nil {
		return nil
	}
	reportAddr, ok := ipidentity.Parse(ip)
	queried, queriedOK := ipidentity.Parse(details.QueriedIP)
	if !ok || !queriedOK || reportAddr != queried || addressFamily(reportAddr) != addressFamily(queried) {
		return invalidProduct("queried ip")
	}
	if details.ObservedAt.IsZero() || details.Method != "agent_public" || details.APITier != "anonymous" {
		return invalidProduct("details method")
	}
	if !validCountry(details.CountryCode) || !validCountry(details.RegisteredCountryCode) || !validCountry(details.ASCountryCode) {
		return invalidProduct("country code")
	}
	if !validOptionalFree(details.Region) || !validOptionalFree(details.City) || !validOptionalFree(details.PostalCode) ||
		!validOptionalFree(details.CompanyName) || !validOptionalFree(details.ASName) || !validTimezone(details.Timezone) {
		return invalidProduct("details text")
	}
	if details.ASDomain != "" && !validHostname(details.ASDomain) {
		return invalidProduct("as domain")
	}
	if details.CompanyDomain != "" && !validHostname(details.CompanyDomain) {
		return invalidProduct("company domain")
	}
	if !validASN(details.ASN) {
		return invalidProduct("asn")
	}
	if details.Route != "" {
		prefix, err := netip.ParsePrefix(details.Route)
		if err != nil || prefix.Addr().Is4() != reportAddr.Is4() || !prefix.Contains(reportAddr) {
			return invalidProduct("route")
		}
	}
	if !validCoordinatePair(details.Latitude, details.Longitude) {
		return invalidProduct("coordinates")
	}
	if len(details.Scores) > ipQualityProductScoreMax {
		return invalidProduct("score count")
	}
	for _, score := range details.Scores {
		if !validRequiredToken(score.ID, ipQualityProductStringMax) ||
			!validOptionalFree(score.Label) || !validOptionalFree(score.RawValue) ||
			!validOptionalFree(score.Unit) || !validOptionalFree(score.SourceLabel) ||
			!validScoreScope(score.Scope) || !validScoreDirection(score.Direction) ||
			!validOptionalFloat(score.Value) || !validOptionalFloat(score.Min) || !validOptionalFloat(score.Max) ||
			!scoreInRange(score) {
			return invalidProduct("score")
		}
	}
	for key, value := range details.FieldStates {
		if _, ok := ipQualityFieldStateKeys[key]; !ok || !validFieldState(value) {
			return invalidProduct("field state")
		}
	}
	if !withinJSONBytes(details, ipQualityProviderDetailsBytesMax) {
		return invalidProduct("details size")
	}
	return nil
}

// ValidateIPQualityServiceChecks validates typed checks. Empty checks are not a
// new wire field. Non-empty checks require product revision 1, a registered
// service, and a check ID registered for that service.
func ValidateIPQualityServiceChecks(service string, checks []IPQualityServiceCheck, productRevision int) error {
	if len(checks) == 0 {
		return nil
	}
	if productRevision != IPQualityProductRevision {
		return invalidProduct("checks without product revision")
	}
	if len(checks) > ipQualityProductChecksMax {
		return invalidProduct("check count")
	}
	allowed, ok := ipQualityServiceChecks[service]
	if !ok {
		return invalidProduct("service")
	}
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		if _, ok := allowed[check.ID]; !ok {
			return invalidProduct("check id")
		}
		if _, ok := seen[check.ID]; ok {
			return invalidProduct("duplicate check")
		}
		seen[check.ID] = struct{}{}
		if !validCheckStatus(check.Status) || !validOptionalFree(check.Label) || !validOptionalFree(check.Rule) ||
			!validOptionalToken(check.ErrorCode, ipQualityProductErrorCodeMax) || !validCheckRegion(check.Region) {
			return invalidProduct("check")
		}
		if check.HTTPStatus != nil && (*check.HTTPStatus < 100 || *check.HTTPStatus > 599) {
			return invalidProduct("http status")
		}
	}
	if !withinJSONBytes(checks, ipQualityServiceChecksBytesMax) {
		return invalidProduct("checks size")
	}
	return nil
}

func invalidProduct(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidIPQualityProduct, reason)
}

func addressFamily(addr netip.Addr) int {
	switch {
	case addr.Is4():
		return 4
	case addr.Is6() && !addr.Is4In6():
		return 6
	default:
		return 0
	}
}

func publicAddress(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Is4In6() || addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsPrivate() || !addr.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicTargets {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// IsGlobalUnicast still treats RFC1918 and several reserved ranges as unicast.
var nonPublicTargets = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func validCoordinatePair(lat, lon *float64) bool {
	if lat == nil && lon == nil {
		return true
	}
	if lat == nil || lon == nil || !validOptionalFloat(lat) || !validOptionalFloat(lon) {
		return false
	}
	return *lat >= -90 && *lat <= 90 && *lon >= -180 && *lon <= 180
}

func validOptionalFloat(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0))
}

func scoreInRange(score IPQualityScore) bool {
	if score.Min != nil && score.Max != nil && *score.Min > *score.Max {
		return false
	}
	if score.Value == nil {
		return true
	}
	if score.Min != nil && *score.Value < *score.Min {
		return false
	}
	if score.Max != nil && *score.Value > *score.Max {
		return false
	}
	return true
}

func validCountry(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 2 {
		return false
	}
	return value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
}

func validASN(value string) bool {
	if value == "" {
		return true
	}
	if !strings.HasPrefix(value, "AS") || len(value) < 3 || len(value) > 12 {
		return false
	}
	number := value[2:]
	if number[0] == '0' {
		return false
	}
	var parsed uint64
	for _, c := range number {
		if c < '0' || c > '9' {
			return false
		}
		parsed = parsed*10 + uint64(c-'0')
	}
	return parsed >= 1 && parsed <= 4294967295
}

func validHostname(value string) bool {
	if value == "" || len(value) > ipQualityProductStringMax || strings.Contains(value, "://") {
		return false
	}
	name := strings.TrimSuffix(value, ".")
	if name == "" || len(name) > 253 || strings.Contains(name, "..") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c > 127 || !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func validTimezone(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > ipQualityProductStringMax || strings.Contains(value, "..") || strings.Contains(value, "://") {
		return false
	}
	for _, r := range value {
		if r > 127 || !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '/' || r == '_' || r == '+' || r == '-') {
			return false
		}
	}
	return true
}

func validOptionalFree(value string) bool {
	return len(value) <= ipQualityProductStringMax && utf8.ValidString(value) && !strings.Contains(value, "://") && !hasControls(value)
}

func validRequiredToken(value string, max int) bool {
	return value != "" && validOptionalToken(value, max)
}

func validOptionalToken(value string, max int) bool {
	if len(value) > max {
		return false
	}
	for _, r := range value {
		if r > 127 || !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func validCheckRegion(value string) bool {
	if value == "" {
		return true
	}
	if len(value) == 2 {
		return validCountry(value)
	}
	return validOptionalFree(value)
}

func hasControls(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validMailStatus(value string) bool {
	switch value {
	case "reachable", "refused", "timeout", "no_mx", "no_address", "unknown", "skipped":
		return true
	default:
		return false
	}
}

func validCheckStatus(value string) bool {
	switch value {
	case "available", "restricted", "unknown":
		return true
	default:
		return false
	}
}

func validScoreScope(value string) bool {
	switch value {
	case "ip", "company_network", "asn":
		return true
	default:
		return false
	}
}

func validScoreDirection(value string) bool {
	switch value {
	case "higher_risk", "higher_trust", "unspecified":
		return true
	default:
		return false
	}
}

func validFieldState(value string) bool {
	switch value {
	case "known", "unsupported", "unknown", "error":
		return true
	default:
		return false
	}
}

func withinJSONBytes(value any, max int) bool {
	payload, err := json.Marshal(value)
	return err == nil && len(payload) <= max
}
