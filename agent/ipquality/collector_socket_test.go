package ipquality_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentipquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

func TestHTTPCollectorDualRoundUsesRealIPv4AndIPv6Sockets(t *testing.T) {
	var mu sync.Mutex
	seen := [2]int{}
	server := func(index int, network, address, ip, region string) *httptest.Server {
		listener, err := net.Listen(network, address)
		if err != nil {
			t.Fatalf("required %s loopback listener: %v", network, err)
		}
		s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil || (net.ParseIP(host).To4() != nil) != (index == 0) {
				t.Errorf("%s request arrived over wrong socket: %s", network, r.RemoteAddr)
			}
			mu.Lock()
			seen[index]++
			mu.Unlock()
			// Preserve the production endpoint identity and path while routing
			// only its socket to a local server. All collector decisions are real.
			cloned := r.Clone(r.Context())
			u := *r.URL
			u.Host = r.Host
			cloned.URL = &u
			response, err := dualFixtureResponse(cloned, ip, region, "AS64500")
			if err != nil {
				http.Error(w, "fixture endpoint mismatch", 500)
				return
			}
			defer response.Body.Close()
			w.WriteHeader(response.StatusCode)
			_, _ = io.Copy(w, response.Body)
		}))
		s.Listener = listener
		s.Start()
		t.Cleanup(s.Close)
		return s
	}
	four := server(0, "tcp4", "127.0.0.1:0", "198.51.100.10", "US")
	six := server(1, "tcp6", "[::1]:0", "2001:db8::10", "JP")
	client := func(network string, s *httptest.Server) *http.Client {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, s.Listener.Addr().String())
		}
		t.Cleanup(transport.CloseIdleConnections)
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			cloned := r.Clone(r.Context())
			u := *r.URL
			u.Scheme = "http"
			cloned.URL = &u
			cloned.Host = r.URL.Host
			return transport.RoundTrip(cloned)
		})}
	}
	client4, client6 := client("tcp4", four), client("tcp6", six)
	dependent := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.String(), "2001:db8") || strings.Contains(r.URL.Query().Get("ip"), "2001:db8") {
			return client6.Transport.RoundTrip(r)
		}
		return client4.Transport.RoundTrip(r)
	})}
	reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: dependent, Client4: client4, Client6: client6, LocalAddresses: dualLocalAddresses}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, Services: []string{"reddit"}}, time.Now())
	if len(reports) != 2 || reports[0].Status != "partial" || reports[1].Status != "partial" || reports[0].IPAddress != "198.51.100.10" || reports[1].IPAddress != "2001:db8::10" {
		t.Fatalf("real socket reports=%#v", reports)
	}
	for index, report := range reports {
		if report.IPVersion != []int{4, 6}[index] || agentapi.ParseCollectionMetadata(report.DiagnosticsJSON).Availability != "available" ||
			len(report.ServiceUnlocks) != 1 || report.ServiceUnlocks[0].Status != "unlocked" {
			t.Fatalf("real socket family terminal=%#v", report)
		}
		assertRejectedDetectorGeoAttempts(t, report)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen != [2]int{6, 6} {
		t.Fatalf("real socket requests=%v, want independent self/provider/service traffic", seen)
	}
}
