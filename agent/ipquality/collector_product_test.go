package ipquality_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	agentquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

func TestCollectorUnnegotiatedProductOmitsEveryNewWireField(t *testing.T) {
	for _, revision := range []int{0, 2} {
		client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return dualFixtureResponse(request, "198.51.100.10", "US", "AS64500")
		})}
		enabled := true
		collector := agentquality.NewHTTPCollector(agentquality.HTTPCollectorOptions{Client: client, LocalAddresses: ipv4LocalAddresses})
		reports := collector.Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, ProductRevision: revision, MailEnabled: &enabled, Services: []string{"reddit"}}, time.Now())
		wire, err := json.Marshal(reports)
		if err != nil {
			t.Fatal(err)
		}
		var decoded []map[string]json.RawMessage
		if err = json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		for _, report := range decoded {
			if _, exists := report["product"]; exists {
				t.Fatalf("revision%d emitted product", revision)
			}
			for _, field := range []struct{ name, forbidden string }{{"provider_results", "details"}, {"service_unlocks", "checks"}} {
				var rows []map[string]json.RawMessage
				if raw, exists := report[field.name]; exists {
					if err = json.Unmarshal(raw, &rows); err != nil {
						t.Fatal(err)
					}
				}
				for _, row := range rows {
					if _, exists := row[field.forbidden]; exists {
						t.Fatalf("revision%d emitted %s", revision, field.forbidden)
					}
				}
			}
		}
		if reports[0].Status != "partial" || len(reports[0].ProviderResults) != 7 || len(reports[0].ServiceUnlocks) != 1 ||
			reports[0].ServiceUnlocks[0].Status != "unlocked" {
			t.Fatalf("old plan disabled existing public collection: %+v", reports[0])
		}
		assertRejectedDetectorGeoAttempts(t, reports[0])
	}
}
