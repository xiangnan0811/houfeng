package ipquality

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

const maxMindTestBody = `{"traits":{"ip_address":"8.8.8.8"},"country":{"iso_code":"US"},"registered_country":{"iso_code":"US"},"city":{"names":{"en":"Mountain View"}},"location":{"latitude":37.4,"longitude":-122.1,"time_zone":"America/Los_Angeles"}}`

func maxMindTestSession(collector *IntelligenceCollector, repo IntelligenceRepository) *intelligenceSession {
	return &intelligenceSession{collector: collector, repo: repo, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{},
		claim: IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4}}}
}

func TestMaxMindAuthenticatedQueryVendorJSONAndPairCache(t *testing.T) {
	repo := newIntelligenceTestRepo()
	collector := NewIntelligenceCollector(repo)
	calls := 0
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		account, key, ok := req.BasicAuth()
		if !ok || account == "" || key == "" || req.Method != http.MethodGet || req.URL.String() != "https://geolite.info/geoip/v2.1/city/8.8.8.8" {
			t.Fatal("GeoLite request omitted Basic authentication or canonical query IP")
		}
		resp := intelligenceResponse(req, 200, maxMindTestBody)
		resp.Header.Set("Content-Type", "application/vnd.maxmind.com-city+json; charset=UTF-8")
		return resp, nil
	})}
	collect := func(account, key string) *intelligenceSession {
		s := maxMindTestSession(collector, repo)
		s.collectMaxMind(account, key)
		if len(s.snapshot.Providers) != 1 || s.snapshot.Providers[0].Status != "success" {
			t.Fatalf("GeoLite normalized sample missing: %+v", s.snapshot)
		}
		d := s.snapshot.Providers[0].Details
		if d.CountryCode != "US" || d.RegisteredCountryCode != "US" || d.City != "Mountain View" || d.IsMobile != nil || len(d.Scores) != 0 {
			t.Fatalf("GeoLite factual fields: %+v", d)
		}
		repo.save(s.snapshot.CacheWrites)
		encoded, _ := json.Marshal(s.snapshot)
		if strings.Contains(string(encoded), key) || strings.Contains(string(encoded), account) || strings.Contains(string(encoded), "Authorization") {
			t.Fatal("GeoLite credential entered serialized snapshot")
		}
		return s
	}
	first := collect("12345678", "test-private-key")
	if len(first.snapshot.CacheWrites) != 1 || first.snapshot.CacheWrites[0].ExpiresAt.Sub(first.snapshot.CacheWrites[0].FetchedAt) != time.Hour {
		t.Fatal("GeoLite cache is not one hour")
	}
	collect("12345678", "test-private-key")
	if calls != 1 {
		t.Fatal("same pair missed cache")
	}
	collect("87654321", "test-private-key")
	collect("87654321", "replacement-private-key")
	if calls != 3 {
		t.Fatal("account or key rotation reused old cache namespace")
	}
}

func TestMaxMindFailuresRetainAnonymousGeoAndNeverCacheFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `{"error":"private-key"}`}, {"forbidden", 403, `{"error":"private-key"}`},
		{"payment", 402, `{}`}, {"quota", 429, `{}`}, {"not found", 404, `{}`},
		{"invalid json", 200, `{`}, {"empty json", 200, `{}`},
		{"wrong IP", 200, strings.ReplaceAll(maxMindTestBody, "8.8.8.8", "1.1.1.1")},
		{"secret echo", 200, strings.ReplaceAll(maxMindTestBody, "Mountain View", "private-key")},
	} {
		t.Run(test.name, func(t *testing.T) {
			collector := NewIntelligenceCollector(nil)
			collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
				return intelligenceResponse(req, test.status, test.body), nil
			})}
			s := maxMindTestSession(collector, nil)
			s.collectMaxMind("12345678", "private-key")
			if len(s.snapshot.Providers) != 1 || s.snapshot.Providers[0].Status != "failure" {
				t.Fatal("invalid GeoLite response became successful")
			}
			if err := ValidateIntelligenceSnapshot(s.snapshot, "8.8.8.8"); err != nil {
				t.Fatal(err)
			}
			for _, entry := range s.snapshot.CacheWrites {
				if entry.Resource != "@quota" {
					t.Fatal("failed GeoLite facts were cached")
				}
			}
			encoded, _ := json.Marshal(s.snapshot)
			if strings.Contains(string(encoded), "private-key") {
				t.Fatal("GeoLite error leaked credential")
			}
			report := ReportWrite{IPAddress: "8.8.8.8", ProviderResults: []ProviderResultWrite{{Provider: "maxmind", Status: "success",
				Details: &agentapi.IPQualityProviderDetails{QueriedIP: "8.8.8.8", Method: "agent_public", APITier: "anonymous", CountryCode: "US", RegisteredCountryCode: "US"}}}}
			view := BuildProductView(report, &s.snapshot)
			if len(view.Providers) != 2 || SelectedProductProviders(view.Providers)[0].Status != "success" || view.Summary.IPType.Status != "native" {
				t.Fatal("failed enhancement overwrote anonymous facts")
			}
		})
	}
}

func TestMaxMindAbsentPairHasNoRequestOrPlaceholder(t *testing.T) {
	collector := NewIntelligenceCollector(nil)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("unconfigured GeoLite requested")
		return nil, nil
	})}
	for _, pair := range [][2]string{{"", ""}, {"12345678", ""}, {"", "private-key"}} {
		s := maxMindTestSession(collector, nil)
		s.collectMaxMind(pair[0], pair[1])
		if len(s.snapshot.Providers) != 0 || len(s.snapshot.SourceOutcomes) != 0 {
			t.Fatal("unconfigured source produced placeholder")
		}
	}
}

func TestMaxMindRedirectCannotForwardBasicCrossOrigin(t *testing.T) {
	first, _ := http.NewRequest(http.MethodGet, "https://geolite.info/geoip/v2.1/city/8.8.8.8", nil)
	req, _ := http.NewRequest(http.MethodGet, "https://api.ipapi.is/", nil)
	req.SetBasicAuth("12345678", "private-key")
	if intelligenceRedirect(req, []*http.Request{first}) == nil || req.Header.Get("Authorization") != "" {
		t.Fatal("GeoLite credentials crossed origin")
	}
}

func TestMaxMindCredentialPairFingerprintIsUnambiguous(t *testing.T) {
	if settings.MaxMindCredentialFingerprint("1", "23") == settings.MaxMindCredentialFingerprint("12", "3") || settings.MaxMindCredentialFingerprint("1", "") != "" {
		t.Fatal("ambiguous or incomplete GeoLite credential namespace")
	}
}

func TestMaxMindShortAccountIDDoesNotMatchOrdinaryGeography(t *testing.T) {
	for _, account := range []string{"1", "12", "123"} {
		t.Run(account, func(t *testing.T) {
			collector := NewIntelligenceCollector(nil)
			collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
				gotAccount, key, ok := req.BasicAuth()
				if !ok || gotAccount != account || key != "private-license-key" {
					t.Fatal("short account ID changed authentication")
				}
				body := strings.Replace(maxMindTestBody, `"city":`, `"postal":{"code":"12345"},"city":`, 1)
				return intelligenceResponse(req, 200, body), nil
			})}
			s := maxMindTestSession(collector, nil)
			s.collectMaxMind(account, "private-license-key")
			if len(s.snapshot.Providers) != 1 || s.snapshot.Providers[0].Status != "success" ||
				s.snapshot.Providers[0].Details.PostalCode != "12345" || s.snapshot.Providers[0].Details.City != "Mountain View" {
				t.Fatalf("account ID substring rejected ordinary geography: %+v", s.snapshot)
			}
			if len(s.snapshot.CacheWrites) != 1 || s.snapshot.CacheWrites[0].Resource == "@quota" {
				t.Fatal("valid short-account geography was not cached")
			}
			if err := ValidateIntelligenceSnapshot(s.snapshot, "8.8.8.8"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMaxMindShortAccountStillRejectsCredentialEchoes(t *testing.T) {
	const account, license = "1", "private-license-key"
	credential := account + ":" + license
	token := base64.StdEncoding.EncodeToString([]byte(credential))
	for _, test := range []struct{ name, echo string }{
		{"license", license},
		{"credential", credential},
		{"authorization", "Basic " + token},
		{"encoded credential", token},
	} {
		t.Run(test.name, func(t *testing.T) {
			collector := NewIntelligenceCollector(nil)
			collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
				body := strings.Replace(maxMindTestBody, "Mountain View", "prefix "+test.echo+" suffix", 1)
				return intelligenceResponse(req, 200, body), nil
			})}
			s := maxMindTestSession(collector, nil)
			s.collectMaxMind(account, license)
			if len(s.snapshot.Providers) != 1 || s.snapshot.Providers[0].Status != "failure" ||
				s.snapshot.Providers[0].ErrorCode != "unsafe_response" {
				t.Fatalf("credential echo became a geographic fact: %+v", s.snapshot)
			}
			for _, entry := range s.snapshot.CacheWrites {
				if entry.Resource != "@quota" {
					t.Fatal("credential-bearing facts were cached")
				}
			}
			wire, err := json.Marshal(s.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), license) || strings.Contains(string(wire), token) ||
				strings.Contains(string(wire), test.echo) {
				t.Fatal("credential echo entered the serialized snapshot")
			}
		})
	}
}
