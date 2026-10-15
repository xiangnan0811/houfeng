package ipquality

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"houfeng/internal/contracts/agentapi"
)

type dnsControl struct{ Expires time.Time }
type intelligenceDNS struct {
	mu       sync.Mutex
	controls map[string]dnsControl
	config   func() (*dns.ClientConfig, error)
	exchange func(context.Context, *dns.Msg, string, string) (*dns.Msg, error)
}

func newIntelligenceDNS() *intelligenceDNS {
	return &intelligenceDNS{
		controls: map[string]dnsControl{},
		config:   func() (*dns.ClientConfig, error) { return dns.ClientConfigFromFile("/etc/resolv.conf") },
		exchange: func(ctx context.Context, msg *dns.Msg, address, network string) (*dns.Msg, error) {
			client := &dns.Client{Net: network, Timeout: 2 * time.Second}
			response, _, err := client.ExchangeContext(ctx, msg, address)
			return response, err
		},
	}
}

type dnsAnswer struct {
	Addresses []netip.Addr
	NXDomain  bool
	TTL       time.Duration
}

func (d *intelligenceDNS) query(ctx context.Context, name string) (dnsAnswer, error) {
	result := dnsAnswer{TTL: 10 * time.Minute}
	config, err := d.config()
	if err != nil || len(config.Servers) == 0 {
		return result, intelligenceError("resolver_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), dns.TypeA)
	msg.RecursionDesired = true
	servers := config.Servers
	if len(servers) > 3 {
		servers = servers[:3]
	}
	for _, server := range servers {
		if _, err := netip.ParseAddr(server); err != nil {
			return result, intelligenceError("resolver_unavailable")
		}
		address := net.JoinHostPort(server, config.Port)
		response, err := d.exchange(ctx, msg, address, "udp")
		if err != nil {
			if ctx.Err() != nil {
				return result, intelligenceError("dns_timeout")
			}
			continue // Only a resolver transport failure permits trying its successor.
		}
		if !validDNSReply(msg, response) {
			return result, intelligenceError("invalid_dns_response")
		}
		if response.Truncated {
			response, err = d.exchange(ctx, msg, address, "tcp")
			if err != nil {
				if ctx.Err() != nil {
					return result, intelligenceError("dns_timeout")
				}
				continue
			}
			if !validDNSReply(msg, response) || response.Truncated {
				return result, intelligenceError("invalid_dns_response")
			}
		}
		if response.Rcode == dns.RcodeNameError {
			if len(response.Answer) != 0 {
				return result, intelligenceError("invalid_dns_response")
			}
			result.NXDomain = true
			for _, rr := range response.Ns {
				if soa, ok := rr.(*dns.SOA); ok {
					ttl := min(soa.Hdr.Ttl, soa.Minttl)
					result.TTL = min(result.TTL, time.Duration(ttl)*time.Second)
				}
			}
			return result, nil
		}
		if response.Rcode != dns.RcodeSuccess {
			return result, intelligenceError("dns_rcode")
		}
		if len(response.Answer) > 32 {
			return result, intelligenceError("dns_answer_limit")
		}
		for _, rr := range response.Answer {
			a, ok := rr.(*dns.A)
			if !ok || !strings.EqualFold(a.Hdr.Name, msg.Question[0].Name) || a.Hdr.Class != dns.ClassINET {
				return result, intelligenceError("unrelated_dns_answer")
			}
			ip, ok := netip.AddrFromSlice(a.A)
			if !ok || !ip.Unmap().Is4() {
				return result, intelligenceError("invalid_dns_answer")
			}
			result.Addresses = append(result.Addresses, ip.Unmap())
			result.TTL = min(result.TTL, time.Duration(a.Hdr.Ttl)*time.Second)
		}
		if len(result.Addresses) == 0 {
			return result, intelligenceError("dns_nodata")
		}
		return result, nil
	}
	return result, intelligenceError("dns_failed")
}
func validDNSReply(query, response *dns.Msg) bool {
	return response != nil && response.Id == query.Id && response.Response && response.Opcode == dns.OpcodeQuery &&
		len(response.Question) == 1 && response.Question[0].Qtype == dns.TypeA && response.Question[0].Qclass == dns.ClassINET &&
		strings.EqualFold(response.Question[0].Name, query.Question[0].Name)
}
func (d *intelligenceDNS) healthy(ctx context.Context, zone, expected string) error {
	d.mu.Lock()
	control, ok := d.controls[zone]
	d.mu.Unlock()
	if ok && control.Expires.After(time.Now()) {
		return nil
	}
	positive, err := d.query(ctx, "2.0.0.127."+zone)
	if err != nil || positive.NXDomain || len(positive.Addresses) != 1 || positive.Addresses[0].String() != expected {
		return intelligenceError("dnsbl_control_failed")
	}
	negative, err := d.query(ctx, "1.0.0.127."+zone)
	if err != nil || !negative.NXDomain {
		return intelligenceError("dnsbl_control_failed")
	}
	ttl := min(10*time.Minute, positive.TTL, negative.TTL)
	d.mu.Lock()
	d.controls[zone] = dnsControl{time.Now().Add(ttl)}
	d.mu.Unlock()
	return nil
}

func (s *intelligenceSession) collectDNSBL(source string, addr netip.Addr) {
	zone, expected, category, reference := "bl.spamcop.net", "127.0.0.2", "spam", "https://www.spamcop.net/bl.shtml"
	if source == "dronebl" {
		zone = "dnsbl.dronebl.org"
		expected = "127.0.0.1" // Health control only; class 1 is not a listed business answer.
		category = "abuse"
		reference = "https://dronebl.org/docs/howtouse"
	}
	row := agentapi.IPQualityBlacklistResult{
		Source: source, Category: category, Status: "unknown", QueryMethod: "dnsbl", ReferenceURL: reference,
		Codes: []string{}, ObservedAt: time.Now().UTC(),
	}
	if !addr.Is4() {
		row.Status = "unsupported"
		s.mu.Lock()
		s.snapshot.Blacklists = append(s.snapshot.Blacklists, row)
		s.snapshot.SourceOutcomes = append(s.snapshot.SourceOutcomes, SourceOutcome{source, "skipped", "ipv6_unsupported", row.ObservedAt})
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 6*time.Second)
	defer cancel()
	err := s.eligible()
	if err == nil {
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		case <-ctx.Done():
			err = intelligenceError("dns_timeout")
		}
	}
	if err == nil {
		err = s.eligible()
	}
	if err == nil {
		err = s.collector.dns.healthy(ctx, zone, expected)
	}
	if err == nil {
		err = s.eligible()
	}
	if err == nil {
		octets := addr.As4()
		name := strconv.Itoa(int(octets[3])) + "." + strconv.Itoa(int(octets[2])) + "." + strconv.Itoa(int(octets[1])) + "." + strconv.Itoa(int(octets[0])) + "." + zone
		var answer dnsAnswer
		answer, err = s.collector.dns.query(ctx, name)
		if err == nil {
			if answer.NXDomain {
				row.Status = "not_listed"
			} else {
				row.Status = "listed"
				for _, a := range answer.Addresses {
					octets := a.As4()
					code := int(octets[3])
					valid := octets[0] == 127 && octets[1] == 0 && octets[2] == 0 &&
						(source == "spamcop" && code == 2 || source == "dronebl" && (code == 2 || code == 3 || code >= 5 && code <= 18 || code == 255))
					if !valid {
						row.Status = "unknown"
						row.Codes = []string{}
						err = intelligenceError("dnsbl_unknown_code")
						break
					}
					row.Codes = append(row.Codes, a.String())
				}
			}
		}
	}
	row.ErrorCode = safeErrorOrEmpty(err)
	s.mu.Lock()
	s.snapshot.Blacklists = append(s.snapshot.Blacklists, row)
	s.mu.Unlock()
	s.outcome(source, err)
}
func safeErrorOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return safeIntelligenceError(err)
}

type dropRecord struct {
	CIDR  string `json:"cidr"`
	SBLID string `json:"sblid"`
	RIR   string `json:"rir"` // Empty represents explicit null provenance in the source dataset.
}
type dropDataset struct {
	Family    int          `json:"family"`
	Entries   []dropRecord `json:"entries"`
	DatasetAt time.Time    `json:"dataset_at"`
	Records   int          `json:"records"`
	Copyright string       `json:"copyright"`
	Terms     string       `json:"terms"`
}

func parseDROP(data []byte, family int) (dropDataset, error) {
	result := dropDataset{Family: family, Entries: []dropRecord{}}
	if len(data) > 8<<20 {
		return result, intelligenceError("response_too_large")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	metadata := false
	seen := map[string]bool{}
	count := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if metadata {
			return result, intelligenceError("drop_after_metadata")
		}
		p, err := decodeIntelligenceJSON(line)
		if err != nil {
			return result, intelligenceError("invalid_drop")
		}
		if text(p["type"]) == "metadata" {
			metadata = true
			records := decimal(p["records"])
			if records == nil {
				return result, intelligenceError("drop_record_count")
			}
			n, err := strconv.Atoi(*records)
			if err != nil || n != count {
				return result, intelligenceError("drop_record_count")
			}
			result.Records = n
			result.Copyright = text(p["copyright"])
			result.Terms = text(p["terms"])
			if ts, ok := p["timestamp"].(json.Number); ok {
				seconds, err := strconv.ParseInt(string(ts), 10, 64)
				if err != nil || seconds <= 0 {
					return result, intelligenceError("drop_timestamp")
				}
				result.DatasetAt = time.Unix(seconds, 0).UTC()
			} else if at := timestamp(p["timestamp"]); at != nil {
				result.DatasetAt = *at
			} else {
				return result, intelligenceError("drop_timestamp")
			}
			continue
		}
		if _, exists := p["type"]; exists {
			return result, intelligenceError("invalid_drop")
		}
		cidr := text(p["cidr"])
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Addr().Is4() != (family == 4) || prefix != prefix.Masked() {
			return result, intelligenceError("drop_invalid_cidr")
		}
		sblid := text(p["sblid"])
		rawRIR, hasRIR := p["rir"]
		rir, rirIsString := rawRIR.(string)
		rir = strings.TrimSpace(rir)
		if sblid == "" || !hasRIR || rawRIR != nil && (!rirIsString || rir == "") {
			return result, intelligenceError("invalid_drop")
		}
		count++
		if !seen[cidr] {
			seen[cidr] = true
			result.Entries = append(result.Entries, dropRecord{cidr, sblid, rir})
		}
	}
	if scanner.Err() != nil || !metadata {
		return result, intelligenceError("drop_incomplete")
	}
	return result, validateDROP(result)
}
func validateDROP(dataset dropDataset) error {
	if dataset.Family != 4 && dataset.Family != 6 || dataset.DatasetAt.IsZero() || dataset.Records < len(dataset.Entries) || dataset.Copyright == "" || dataset.Terms == "" {
		return intelligenceError("invalid_drop_metadata")
	}
	if len(dataset.Copyright) > 2048 || len(dataset.Terms) > 2048 {
		return intelligenceError("invalid_drop_metadata")
	}
	if dataset.DatasetAt.Year() < 1 || dataset.DatasetAt.Year() > 9999 || strings.ContainsAny(dataset.Copyright, "\x00\r\n") || strings.ContainsAny(dataset.Terms, "\x00\r\n") {
		return intelligenceError("invalid_drop_metadata")
	}
	for _, entry := range dataset.Entries {
		prefix, err := netip.ParsePrefix(entry.CIDR)
		if err != nil || prefix.Addr().Is4() != (dataset.Family == 4) || prefix != prefix.Masked() || entry.SBLID == "" || len(entry.SBLID) > 256 || len(entry.RIR) > 256 {
			return intelligenceError("invalid_drop")
		}
		if strings.ContainsAny(entry.SBLID, "\x00\r\n") || strings.ContainsAny(entry.RIR, "\x00\r\n") {
			return intelligenceError("invalid_drop")
		}
	}
	return nil
}
func (s *intelligenceSession) collectDROP(addr netip.Addr) {
	source := "spamhaus-drop"
	family := 6
	if addr.Is4() {
		family = 4
	}
	resource := "ipv" + strconv.Itoa(family)
	endpoint := "https://www.spamhaus.org/drop/drop_v" + strconv.Itoa(family) + ".json"
	row := agentapi.IPQualityBlacklistResult{
		Source: source, Category: "network", Status: "unknown", QueryMethod: "dataset", ReferenceURL: endpoint,
		Codes: []string{}, ObservedAt: time.Now().UTC(),
	}
	var dataset dropDataset
	var err error
	if !s.load(source, resource, "", &dataset) {
		var data []byte
		data, err = s.request(source, resource, "", http.MethodGet, endpoint, nil, nil, 8<<20)
		if err == nil {
			dataset, err = parseDROP(data, family)
		}
		if err == nil {
			s.cache(source, resource, "", dataset, time.Hour)
		}
	}
	if err == nil && dataset.Family != family {
		err = errors.New("drop family mismatch")
	}
	if err == nil {
		row.Status = "not_listed"
		row.DatasetAt = &dataset.DatasetAt
		for _, entry := range dataset.Entries {
			prefix, _ := netip.ParsePrefix(entry.CIDR)
			if prefix.Contains(addr) {
				row.Status = "listed"
				row.MatchedRange = entry.CIDR
				row.Codes = []string{entry.SBLID}
				break
			}
		}
	}
	row.ErrorCode = safeErrorOrEmpty(err)
	s.mu.Lock()
	s.snapshot.Blacklists = append(s.snapshot.Blacklists, row)
	s.mu.Unlock()
	s.outcome(source, err)
}
