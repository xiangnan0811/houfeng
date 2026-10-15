package ipquality

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errIntelligenceInactive = errors.New("lifecycle_inactive")

type intelligenceError string

func (e intelligenceError) Error() string { return string(e) }

var intelligenceHosts = map[string]bool{
	"stat.ripe.net": true, "data.iana.org": true, "api.ipapi.is": true, "api.abuseipdb.com": true, "www.spamhaus.org": true,
	"rdap.arin.net": true, "rdap.db.ripe.net": true, "rdap.apnic.net": true, "rdap.lacnic.net": true, "rdap.afrinic.net": true,
	"geolite.info": true,
}
var rirHosts = map[string]string{
	"rdap.arin.net": "ARIN", "rdap.db.ripe.net": "RIPE NCC", "rdap.apnic.net": "APNIC", "rdap.lacnic.net": "LACNIC", "rdap.afrinic.net": "AFRINIC",
}
var nonPublicIntelligencePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("fec0::/10"),
}

func publicIntelligenceAddress(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range nonPublicIntelligencePrefixes {
		if prefix.Contains(a) {
			return false
		}
	}
	return true
}

func newIntelligenceHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	transport := &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8,
		DialContext: pinnedIntelligenceDial(net.DefaultResolver.LookupNetIP, dialer.DialContext),
	}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: intelligenceRedirect}
}

func pinnedIntelligenceDial(resolve func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || !intelligenceHosts[host] {
			return nil, intelligenceError("target_denied")
		}
		addresses, err := resolve(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, intelligenceError("dns_failed")
		}
		// Reject mixed public/private answers too. Dial receives only the numeric,
		// validated address, preventing a second lookup and DNS rebinding.
		for _, a := range addresses {
			if !publicIntelligenceAddress(a) {
				return nil, intelligenceError("non_public_target")
			}
		}
		for _, a := range addresses {
			conn, err := dial(ctx, network, net.JoinHostPort(a.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, intelligenceError("connect_failed")
	}
}

func intelligenceRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) > 3 || !validIntelligenceURL(req.URL) {
		return intelligenceError("redirect_denied")
	}
	first := via[0].URL.Hostname()
	if req.URL.Hostname() != first {
		req.Header.Del("Authorization")
		req.Header.Del("Key")
		if rirHosts[first] == "" || rirHosts[req.URL.Hostname()] == "" {
			return intelligenceError("redirect_denied")
		}
	}
	return nil
}
func validIntelligenceURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && intelligenceHosts[u.Hostname()]
}

func (s *intelligenceSession) request(source, resource, fingerprint, method, endpoint string, body io.Reader, headers http.Header, limit int64) ([]byte, error) {
	if err := s.eligible(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	blocked := s.quotas[source+":"+fingerprint].After(time.Now())
	s.mu.Unlock()
	if blocked {
		return nil, intelligenceError("source_rate_limited")
	}
	if s.repo != nil {
		quota, err := s.repo.LoadIPQualitySourceCache(s.ctx, source, "@quota", fingerprint)
		if err != nil {
			return nil, intelligenceError("cache_failed")
		}
		if quota != nil && quota.BlockedUntil != nil && quota.BlockedUntil.After(time.Now()) {
			return nil, intelligenceError("source_rate_limited")
		}
	}
	select {
	case s.slots <- struct{}{}:
	case <-s.ctx.Done():
		return nil, intelligenceError("timeout")
	}
	defer func() { <-s.slots }()
	if err := s.eligible(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	blocked = s.quotas[source+":"+fingerprint].After(time.Now())
	s.mu.Unlock()
	if blocked {
		return nil, intelligenceError("source_rate_limited")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil || !validIntelligenceURL(req.URL) {
		return nil, intelligenceError("target_denied")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Houfeng-IPQuality/1")
	resp, err := s.collector.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, intelligenceError("timeout")
		}
		var known intelligenceError
		if errors.As(err, &known) {
			return nil, known
		}
		return nil, intelligenceError("request_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 || resp.StatusCode == 403 {
		now := time.Now().UTC()
		until := quotaUntil(resp.Header.Get("Retry-After"), now)
		s.addCache(SourceCacheEntry{SourceID: source, Resource: "@quota", CredentialFingerprint: fingerprint, FetchedAt: now, ExpiresAt: until, BlockedUntil: &until, PayloadJSON: []byte(`{}`)})
		return nil, intelligenceError("source_rate_limited")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, intelligenceError("http_failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, intelligenceError("body_failed")
	}
	if int64(len(data)) > limit {
		return nil, intelligenceError("response_too_large")
	}
	return data, nil
}

func quotaUntil(raw string, now time.Time) time.Time {
	duration := time.Hour
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && seconds >= 0 {
		duration = time.Duration(min(seconds, int64(86400))) * time.Second
	} else if at, err := http.ParseTime(raw); err == nil {
		duration = at.Sub(now)
	}
	duration = max(time.Minute, min(duration, 24*time.Hour))
	return now.Add(duration)
}
