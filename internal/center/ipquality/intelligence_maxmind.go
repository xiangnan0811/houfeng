package ipquality

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/netip"
	"reflect"
	"time"

	"houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

func (s *intelligenceSession) collectMaxMind(accountID, licenseKey string) {
	const source = "maxmind"
	target := s.claim.Report.IPAddress
	fp := settings.MaxMindCredentialFingerprint(accountID, licenseKey)
	if fp == "" {
		return
	}
	var row ProviderResultRead
	if s.load(source, target, fp, &row) {
		s.mu.Lock()
		s.snapshot.Providers = append(s.snapshot.Providers, row)
		s.mu.Unlock()
		s.outcome(source, nil)
		return
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(accountID+":"+licenseKey))
	data, err := s.request(source, target, fp, http.MethodGet,
		"https://geolite.info/geoip/v2.1/city/"+target, nil, http.Header{"Authorization": []string{auth}}, 2<<20)
	if err == nil {
		row, err = parseMaxMind(data, target, time.Now().UTC())
		if err == nil {
			err = ValidateIntelligenceSnapshot(IntelligenceSnapshot{Providers: []ProviderResultRead{row}}, target)
			// The numeric account ID is public identity, not a secret: a short
			// ID may legitimately occur in coordinates, IPs or postal codes.
			if err == nil && (credentialEcho(reflect.ValueOf(row), licenseKey) || credentialEcho(reflect.ValueOf(row), accountID+":"+licenseKey) ||
				credentialEcho(reflect.ValueOf(row), auth) || credentialEcho(reflect.ValueOf(row), auth[len("Basic "):])) {
				err = intelligenceError("unsafe_response")
			}
		}
	}
	if err == nil {
		s.cache(source, target, fp, row, time.Hour)
	} else {
		row = ProviderResultRead{Provider: source, Status: "failure", SourceType: "default", ErrorCode: safeIntelligenceError(err),
			Details: &agentapi.IPQualityProviderDetails{QueriedIP: target, ObservedAt: time.Now().UTC(), Method: "center_api", APITier: "authenticated",
				Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{}},
		}
	}
	s.mu.Lock()
	s.snapshot.Providers = append(s.snapshot.Providers, row)
	s.mu.Unlock()
	s.outcome(source, err)
}

func parseMaxMind(data []byte, target string, observedAt time.Time) (ProviderResultRead, error) {
	payload, err := decodeIntelligenceJSON(data)
	if err != nil {
		return ProviderResultRead{}, err
	}
	traits := object(payload["traits"])
	ip, err := netip.ParseAddr(text(traits["ip_address"]))
	want, targetErr := netip.ParseAddr(target)
	if err != nil || targetErr != nil || ip.Unmap() != want.Unmap() {
		return ProviderResultRead{}, intelligenceError("target_mismatch")
	}
	d := &agentapi.IPQualityProviderDetails{
		QueriedIP: target, ObservedAt: observedAt, Method: "center_api", APITier: "authenticated",
		CountryCode:           country(object(payload["country"])["iso_code"]),
		RegisteredCountryCode: country(object(payload["registered_country"])["iso_code"]),
		City:                  text(object(object(payload["city"])["names"])["en"]),
		PostalCode:            text(object(payload["postal"])["code"]),
		Timezone:              text(object(payload["location"])["time_zone"]),
		Scores:                []agentapi.IPQualityScore{}, FieldStates: map[string]string{},
	}
	if subdivisions, ok := payload["subdivisions"].([]any); ok && len(subdivisions) > 0 {
		d.Region = text(object(object(subdivisions[0])["names"])["en"])
	}
	for key, dest := range map[string]**float64{"latitude": &d.Latitude, "longitude": &d.Longitude} {
		if number, ok := object(payload["location"])[key].(json.Number); ok {
			value, e := number.Float64()
			if e != nil {
				return ProviderResultRead{}, intelligenceError("invalid_location")
			}
			*dest = &value
		}
	}
	for key, value := range map[string]string{"country_code": d.CountryCode, "registered_country_code": d.RegisteredCountryCode,
		"city": d.City, "region": d.Region, "postal_code": d.PostalCode, "timezone": d.Timezone} {
		if value != "" {
			d.FieldStates[key] = "known"
		}
	}
	if d.Latitude != nil {
		d.FieldStates["latitude"] = "known"
	}
	if d.Longitude != nil {
		d.FieldStates["longitude"] = "known"
	}
	if len(d.FieldStates) == 0 {
		return ProviderResultRead{}, intelligenceError("invalid_provider_payload")
	}
	return ProviderResultRead{Provider: "maxmind", Status: "success", SourceType: "api", RegionCode: d.CountryCode, Details: d}, nil
}
