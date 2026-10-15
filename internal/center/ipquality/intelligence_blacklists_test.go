package ipquality

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"houfeng/internal/contracts/agentapi"
)

func testIntelligenceTime() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }
func testDNSSession(d *intelligenceDNS) *intelligenceSession {
	return &intelligenceSession{collector: &IntelligenceCollector{dns: d}, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{}, snapshot: IntelligenceSnapshot{Blacklists: []agentapi.IPQualityBlacklistResult{}, SourceOutcomes: []SourceOutcome{}}}
}
func fixtureIntelligenceDNS(business string, code int, controlFailure bool) *intelligenceDNS {
	d := newIntelligenceDNS()
	d.config = func() (*dns.ClientConfig, error) {
		return &dns.ClientConfig{Servers: []string{"127.0.0.1"}, Port: "53"}, nil
	}
	d.exchange = func(ctx context.Context, q *dns.Msg, address, network string) (*dns.Msg, error) {
		response := new(dns.Msg)
		response.SetReply(q)
		name := q.Question[0].Name
		answer := ""
		switch {
		case strings.HasPrefix(name, "2.0.0.127."):
			if controlFailure {
				response.Rcode = dns.RcodeServerFailure
				break
			}
			answer = "127.0.0.2"
			if strings.HasSuffix(name, "dnsbl.dronebl.org.") {
				answer = "127.0.0.1"
			}
		case strings.HasPrefix(name, "1.0.0.127."):
			response.Rcode = dns.RcodeNameError
		default:
			response.Rcode = code
			answer = business
			if code != dns.RcodeSuccess {
				answer = ""
			}
		}
		if answer != "" {
			response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP(answer).To4()}}
		}
		return response, nil
	}
	return d
}

func TestIntelligenceDNSBLControlsAndBusinessClassification(t *testing.T) {
	tests := []struct {
		name, source, business, want string
		rcode                        int
		badControl                   bool
	}{
		{"healthy NXDOMAIN", "spamcop", "", "not_listed", dns.RcodeNameError, false},
		{"listed SpamCop", "spamcop", "127.0.0.2", "listed", dns.RcodeSuccess, false},
		{"DroneBL SOCKS class8", "dronebl", "127.0.0.8", "listed", dns.RcodeSuccess, false},
		{"control failure", "spamcop", "", "unknown", dns.RcodeNameError, true},
		{"SERVFAIL", "spamcop", "", "unknown", dns.RcodeServerFailure, false},
		{"provider error code", "spamcop", "127.255.255.254", "unknown", dns.RcodeSuccess, false},
		{"DroneBL control answer is not business class", "dronebl", "127.0.0.1", "unknown", dns.RcodeSuccess, false},
		{"NODATA", "spamcop", "", "unknown", dns.RcodeSuccess, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testDNSSession(fixtureIntelligenceDNS(tt.business, tt.rcode, tt.badControl))
			s.collectDNSBL(tt.source, netip.MustParseAddr("8.8.8.8"))
			if len(s.snapshot.Blacklists) != 1 || s.snapshot.Blacklists[0].Status != tt.want {
				t.Fatalf("result: %+v", s.snapshot)
			}
			row := s.snapshot.Blacklists[0]
			if tt.want == "listed" && (len(row.Codes) != 1 || row.Codes[0] != tt.business) {
				t.Fatalf("class code not retained: %+v", row)
			}
			if tt.want == "unknown" && len(row.Codes) != 0 {
				t.Fatal("unknown keeps listed evidence")
			}
		})
	}
}

func TestIntelligenceDNSBLIPv6NeverQueriesIPv4(t *testing.T) {
	d := newIntelligenceDNS()
	d.config = func() (*dns.ClientConfig, error) {
		t.Error("IPv6 queried unsupported DNSBL")
		return nil, errors.New("unexpected")
	}
	s := testDNSSession(d)
	s.collectDNSBL("spamcop", netip.MustParseAddr("2001:4860:4860::8888"))
	if s.snapshot.Blacklists[0].Status != "unsupported" || s.snapshot.SourceOutcomes[0].Status != "skipped" {
		t.Fatalf("IPv6: %+v", s.snapshot)
	}
}

func TestIntelligenceDNSRealUDPToTCPFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	var tcpQueries atomic.Int32
	handler := dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		response := new(dns.Msg)
		response.SetReply(q)
		if w.LocalAddr().Network() == "udp" {
			response.Truncated = true
		} else {
			tcpQueries.Add(1)
			response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 5}, A: net.IPv4(127, 0, 0, 2)}}
		}
		_ = w.WriteMsg(response)
	})
	udpServer := &dns.Server{PacketConn: udp, Handler: handler}
	tcpServer := &dns.Server{Listener: listener, Handler: handler}
	ready := make(chan struct{}, 2)
	udpServer.NotifyStartedFunc = func() { ready <- struct{}{} }
	tcpServer.NotifyStartedFunc = func() { ready <- struct{}{} }
	go func() { _ = udpServer.ActivateAndServe() }()
	go func() { _ = tcpServer.ActivateAndServe() }()
	t.Cleanup(func() { _ = udpServer.Shutdown(); _ = tcpServer.Shutdown() })
	for range 2 {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("DNS fixture did not start")
		}
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	d := newIntelligenceDNS()
	d.config = func() (*dns.ClientConfig, error) {
		return &dns.ClientConfig{Servers: []string{"127.0.0.1"}, Port: port}, nil
	}
	answer, err := d.query(context.Background(), "2.0.0.127.bl.spamcop.net.")
	if err != nil || len(answer.Addresses) != 1 || answer.Addresses[0].String() != "127.0.0.2" || tcpQueries.Load() != 1 || answer.TTL != 5*time.Second {
		t.Fatalf("TCP/TTL: %+v %v queries=%d", answer, err, tcpQueries.Load())
	}
}

func TestIntelligenceDNSRejectsUnrelatedAnswersAndOnlyFailsOverNetwork(t *testing.T) {
	for _, mode := range []string{"id", "question", "answer", "servfail", "network"} {
		t.Run(mode, func(t *testing.T) {
			d := newIntelligenceDNS()
			d.config = func() (*dns.ClientConfig, error) {
				return &dns.ClientConfig{Servers: []string{"127.0.0.1", "127.0.0.2"}, Port: "53"}, nil
			}
			calls := 0
			d.exchange = func(ctx context.Context, q *dns.Msg, address, network string) (*dns.Msg, error) {
				calls++
				if mode == "network" && calls == 1 {
					return nil, errors.New("network failed")
				}
				r := new(dns.Msg)
				r.SetReply(q)
				r.Question = append([]dns.Question{}, q.Question...)
				if mode == "id" {
					r.Id++
				}
				if mode == "question" {
					r.Question[0].Name = "unrelated.example."
				}
				if mode == "servfail" {
					r.Rcode = dns.RcodeServerFailure
					return r, nil
				}
				name := q.Question[0].Name
				if mode == "answer" {
					name = "unrelated.example."
				}
				r.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(127, 0, 0, 2)}}
				return r, nil
			}
			_, err := d.query(context.Background(), "2.0.0.127.bl.spamcop.net.")
			if mode == "network" {
				if err != nil || calls != 2 {
					t.Fatalf("network failover: calls=%d err=%v", calls, err)
				}
			} else if err == nil || calls != 1 {
				t.Fatalf("untrusted answer/failover: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestIntelligenceDROPRequiresCompleteMetadata(t *testing.T) {
	valid := `{"cidr":"8.8.8.0/24","sblid":"SBL123","rir":"arin"}` + "\n" + `{"type":"metadata","timestamp":1791979200,"records":1,"copyright":"Copyright Spamhaus","terms":"https://www.spamhaus.org/terms/"}` + "\n"
	dataset, err := parseDROP([]byte(valid), 4)
	if err != nil || len(dataset.Entries) != 1 || dataset.Records != 1 || dataset.Copyright == "" || dataset.Terms == "" {
		t.Fatalf("valid DROP: %+v %v", dataset, err)
	}
	for name, input := range map[string]string{
		"no metadata":       strings.Split(valid, "\n")[0] + "\n",
		"count mismatch":    strings.Replace(valid, `"records":1`, `"records":2`, 1),
		"bad cidr":          strings.Replace(valid, "8.8.8.0/24", "8.8.8.99/24", 1),
		"wrong family":      strings.Replace(valid, "8.8.8.0/24", "2001:4860::/32", 1),
		"after metadata":    valid + strings.Split(valid, "\n")[0],
		"missing copyright": strings.Replace(valid, `"Copyright Spamhaus"`, `""`, 1),
		"missing rir":       strings.Replace(valid, `,"rir":"arin"`, ``, 1),
		"numeric rir":       strings.Replace(valid, `"rir":"arin"`, `"rir":123`, 1),
		"boolean rir":       strings.Replace(valid, `"rir":"arin"`, `"rir":false`, 1),
		"object rir":        strings.Replace(valid, `"rir":"arin"`, `"rir":{}`, 1),
		"array rir":         strings.Replace(valid, `"rir":"arin"`, `"rir":[]`, 1),
		"empty rir":         strings.Replace(valid, `"rir":"arin"`, `"rir":""`, 1),
		"whitespace rir":    strings.Replace(valid, `"rir":"arin"`, `"rir":"  "`, 1),
		"control in rir":    strings.Replace(valid, `"rir":"arin"`, `"rir":"ar\nin"`, 1),
		"control in sbl":    strings.Replace(valid, `"sblid":"SBL123"`, `"sblid":"SBL\u0000123"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDROP([]byte(input), 4); err == nil {
				t.Fatal("incomplete/invalid DROP accepted")
			}
			collector := NewIntelligenceCollector(nil)
			collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
				return intelligenceResponse(req, http.StatusOK, input), nil
			})}
			s := &intelligenceSession{collector: collector, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
			s.collectDROP(netip.MustParseAddr("8.8.8.8"))
			if len(s.snapshot.Blacklists) != 1 || s.snapshot.Blacklists[0].Status != "unknown" || s.snapshot.Blacklists[0].DatasetAt != nil || len(s.snapshot.CacheWrites) != 0 {
				t.Fatalf("invalid dataset supplied facts or cache: %+v", s.snapshot)
			}
		})
	}
	data, _ := json.Marshal(dataset)
	if err := ValidateIntelligenceCacheEntry(SourceCacheEntry{SourceID: "spamhaus-drop", Resource: "ipv4", PayloadJSON: data}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIntelligenceCacheEntry(SourceCacheEntry{SourceID: "spamhaus-drop", Resource: "ipv6", PayloadJSON: data}); err == nil {
		t.Fatal("cross-family cache accepted")
	}
}

func TestIntelligenceDROPNullRIRPreservesMatchesAndCache(t *testing.T) {
	body := `{"cidr":"45.138.12.0/24","sblid":"SBL700608","rir":null}` + "\n" +
		`{"cidr":"1.0.0.0/8","sblid":"SBL1","rir":"apnic"}` + "\n" +
		`{"type":"metadata","timestamp":1791979200,"records":2,"copyright":"Copyright Spamhaus","terms":"https://www.spamhaus.org/terms/"}`
	for _, target := range []struct {
		ip, status, matchedRange, sbl string
	}{
		{"45.138.12.1", "listed", "45.138.12.0/24", "SBL700608"},
		{"1.1.1.1", "listed", "1.0.0.0/8", "SBL1"},
		{"8.8.8.8", "not_listed", "", ""},
	} {
		t.Run(target.ip, func(t *testing.T) {
			repo := newIntelligenceTestRepo()
			collector := NewIntelligenceCollector(repo)
			requests := 0
			collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.String() != "https://www.spamhaus.org/drop/drop_v4.json" {
					t.Errorf("wrong DROP endpoint: %s", req.URL)
				}
				return intelligenceResponse(req, http.StatusOK, body), nil
			})}
			for _, mode := range []string{"fresh", "cached"} {
				t.Run(mode, func(t *testing.T) {
					s := &intelligenceSession{collector: collector, repo: repo, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
					s.collectDROP(netip.MustParseAddr(target.ip))
					if len(s.snapshot.Blacklists) != 1 {
						t.Fatalf("missing DROP result: %+v", s.snapshot)
					}
					row := s.snapshot.Blacklists[0]
					if row.Status != target.status || row.MatchedRange != target.matchedRange || row.ErrorCode != "" || row.DatasetAt == nil {
						t.Fatalf("DROP result: %+v", row)
					}
					if target.sbl == "" && len(row.Codes) != 0 || target.sbl != "" && (len(row.Codes) != 1 || row.Codes[0] != target.sbl) {
						t.Fatalf("DROP match code: %+v", row)
					}
					if mode == "fresh" {
						if len(s.snapshot.CacheWrites) != 1 {
							t.Fatalf("verified dataset not cached: %+v", s.snapshot.CacheWrites)
						}
						entry := s.snapshot.CacheWrites[0]
						if err := ValidateIntelligenceCacheEntry(entry); err != nil {
							t.Fatalf("null RIR cache rejected: %v", err)
						}
						var dataset dropDataset
						if err := json.Unmarshal(entry.PayloadJSON, &dataset); err != nil {
							t.Fatal(err)
						}
						if len(dataset.Entries) != 2 || dataset.Records != 2 || dataset.Entries[0].CIDR != "45.138.12.0/24" || dataset.Entries[0].SBLID != "SBL700608" || dataset.Entries[0].RIR != "" || dataset.Entries[1].RIR != "apnic" {
							t.Fatalf("null provenance lost or fabricated: %+v", dataset)
						}
						repo.save(s.snapshot.CacheWrites)
					} else if len(s.snapshot.CacheWrites) != 0 {
						t.Fatal("cached lookup unexpectedly wrote a new dataset")
					}
				})
			}
			if requests != 1 {
				t.Fatalf("verified cache not reused: requests=%d", requests)
			}
		})
	}
}

func TestIntelligenceDNSControlCacheHonorsTTL(t *testing.T) {
	d := fixtureIntelligenceDNS("", dns.RcodeNameError, false)
	exchange := d.exchange
	calls := 0
	d.exchange = func(ctx context.Context, q *dns.Msg, address, network string) (*dns.Msg, error) {
		calls++
		return exchange(ctx, q, address, network)
	}
	if err := d.healthy(context.Background(), "bl.spamcop.net", "127.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if err := d.healthy(context.Background(), "bl.spamcop.net", "127.0.0.2"); err != nil || calls != 2 {
		t.Fatalf("valid control not cached: %d %v", calls, err)
	}
	d.controls["bl.spamcop.net"] = dnsControl{time.Now().Add(-time.Second)}
	if err := d.healthy(context.Background(), "bl.spamcop.net", "127.0.0.2"); err != nil || calls != 4 {
		t.Fatalf("expired control reused: %d %v", calls, err)
	}
}
