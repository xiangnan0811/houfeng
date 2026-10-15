package ipquality

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
)

var mailProviders = [...]struct{ id, domain string }{
	{"gmail", "gmail.com"}, {"outlook", "outlook.com"}, {"yahoo", "yahoo.com"}, {"apple", "me.com"},
	{"mailru", "mail.ru"}, {"aol", "aol.com"}, {"gmx", "gmx.com"}, {"mailcom", "mail.com"},
	{"163", "163.com"}, {"sohu", "sohu.com"}, {"sina", "sina.com"}, {"qq", "qq.com"},
}

type mailResolver interface {
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupIP(context.Context, string, string) ([]net.IP, error)
	LookupAddr(context.Context, string) ([]string, error)
}

type mailProbe struct {
	resolver mailResolver
	dial     func(context.Context, string, string) (net.Conn, error)
}

func skippedMailResults(observedAt time.Time, errorCode string) []agentapi.IPQualityMailResult {
	results := make([]agentapi.IPQualityMailResult, len(mailProviders))
	for i, provider := range mailProviders {
		results[i] = agentapi.IPQualityMailResult{Provider: provider.id, Domain: provider.domain, Status: "skipped", ErrorCode: errorCode, ObservedAt: observedAt.UTC()}
	}
	return results
}

// The caller owns the shared six-slot pool across both address families. The
// per-provider budget starts only after admission; queue time uses the parent.
func collectMail(ctx context.Context, ip string, family int, observedAt time.Time, slots chan struct{}) []agentapi.IPQualityMailResult {
	probe := mailProbe{resolver: net.DefaultResolver, dial: (&net.Dialer{}).DialContext}
	return probe.collect(ctx, ip, family, observedAt, slots)
}

func (p mailProbe) collect(ctx context.Context, ip string, family int, observedAt time.Time, slots chan struct{}) []agentapi.IPQualityMailResult {
	address, err := netip.ParseAddr(ip)
	if err != nil || !mailPublicAddress(address) || (family != 4 && family != 6) || address.Is4() != (family == 4) {
		return skippedMailResults(observedAt, "invalid_target")
	}
	results := skippedMailResults(observedAt, "")
	var workers sync.WaitGroup
	for i := range results {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				results[i] = p.provider(probeCtx, results[i], family)
			case <-ctx.Done():
				results[i].Status, results[i].ErrorCode = mailContextStatus(ctx)
			}
		}(i)
	}
	workers.Wait()
	return results
}

func mailContextStatus(ctx context.Context) (string, string) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "unknown", "canceled"
	}
	return "timeout", "timeout"
}

func (p mailProbe) provider(ctx context.Context, result agentapi.IPQualityMailResult, family int) agentapi.IPQualityMailResult {
	start := time.Now()
	finish := func(status, code string) agentapi.IPQualityMailResult {
		result.Status, result.ErrorCode = status, code
		latency := int(time.Since(start).Milliseconds())
		result.LatencyMS = &latency
		return result
	}
	if ctx.Err() != nil {
		status, code := mailContextStatus(ctx)
		return finish(status, code)
	}
	mxs, err := p.resolver.LookupMX(ctx, result.Domain)
	if err != nil {
		if ctx.Err() != nil {
			status, code := mailContextStatus(ctx)
			return finish(status, code)
		}
		var dnsError *net.DNSError
		if !errors.As(err, &dnsError) || !dnsError.IsNotFound {
			return finish("unknown", "dns_failed")
		}
	}
	if len(mxs) == 0 {
		mxs = []*net.MX{{Host: result.Domain}}
	}
	candidates := make([]net.MX, 0, len(mxs))
	for _, mx := range mxs {
		if mx == nil {
			return finish("unknown", "invalid_mx")
		}
		if mx.Host == "." {
			if len(mxs) == 1 && mx.Pref == 0 {
				return finish("no_mx", "")
			}
			return finish("unknown", "invalid_mx")
		}
		host := strings.ToLower(strings.TrimSuffix(mx.Host, "."))
		if !mailHostname(host) {
			return finish("unknown", "invalid_mx")
		}
		candidates = append(candidates, net.MX{Host: host, Pref: mx.Pref})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Pref != candidates[j].Pref {
			return candidates[i].Pref < candidates[j].Pref
		}
		return candidates[i].Host < candidates[j].Host
	})
	unique := candidates[:0]
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if !seen[candidate.Host] {
			unique = append(unique, candidate)
			seen[candidate.Host] = true
		}
	}
	truncated := len(unique) > 20
	if truncated {
		unique = unique[:20]
	}
	network, lookup := "tcp4", "ip4"
	if family == 6 {
		network, lookup = "tcp6", "ip6"
	}
	connections, dnsFailed, nonPublic := 0, false, false
	lastStatus, lastCode := "unknown", "connection_failed"
	for _, mx := range unique {
		if ctx.Err() != nil {
			status, code := mailContextStatus(ctx)
			return finish(status, code)
		}
		addresses, err := p.resolver.LookupIP(ctx, lookup, mx.Host)
		if err != nil {
			if ctx.Err() != nil {
				status, code := mailContextStatus(ctx)
				return finish(status, code)
			}
			var dnsError *net.DNSError
			if !errors.As(err, &dnsError) || !dnsError.IsNotFound {
				dnsFailed = true
			}
			continue
		}
		var target netip.Addr
		for _, ip := range addresses {
			address, ok := netip.AddrFromSlice(ip)
			if !ok {
				dnsFailed = true
				continue
			}
			address = address.Unmap()
			if address.Is4() != (family == 4) {
				continue
			}
			if !mailPublicAddress(address) {
				nonPublic = true
				continue
			}
			target = address
			break
		}
		if !target.IsValid() {
			continue
		}
		result.MXHost, result.TargetIP = mx.Host, target.String()
		preference := mx.Pref
		result.MXPreference = &preference
		connections++
		connection, err := p.dial(ctx, network, net.JoinHostPort(target.String(), "25"))
		if err != nil {
			lastCode = errorCodeForHTTPError(err)
			lastStatus = "unknown"
			if lastCode == "timeout" {
				lastStatus = "timeout"
			}
			if ctx.Err() != nil {
				lastStatus, lastCode = mailContextStatus(ctx)
			}
			if connections == 2 {
				return finish(lastStatus, lastCode)
			}
			continue
		}
		status, code, greeting := readMailGreeting(ctx, connection)
		result.GreetingCode = greeting
		return finish(status, code)
	}
	if ctx.Err() != nil {
		status, code := mailContextStatus(ctx)
		return finish(status, code)
	}
	if connections > 0 {
		return finish(lastStatus, lastCode)
	}
	if truncated {
		return finish("unknown", "mx_candidates_truncated")
	}
	if dnsFailed {
		return finish("unknown", "dns_failed")
	}
	if nonPublic {
		return finish("unknown", "non_public_target")
	}
	return finish("no_address", "")
}

// Every connection targets a previously validated literal IP. DNS cannot change
// the destination between validation and dial, and tcp6 never falls back to v4.
func mailPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is4() {
		bytes := address.As4()
		if bytes[0] == 0 || bytes[0] == 10 || bytes[0] == 127 || bytes[0] >= 224 || bytes[0] == 100 && bytes[1] >= 64 && bytes[1] <= 127 || bytes[0] == 169 && bytes[1] == 254 || bytes[0] == 198 && (bytes[1] == 18 || bytes[1] == 19) {
			return false
		}
		if bytes[0] == 192 && bytes[1] == 0 && (bytes[2] == 0 || bytes[2] == 2) || bytes[0] == 198 && bytes[1] == 51 && bytes[2] == 100 || bytes[0] == 203 && bytes[1] == 0 && bytes[2] == 113 {
			return false
		}
	} else {
		bytes := address.As16()
		if bytes[0]&0xe0 != 0x20 || bytes[0] == 0x20 && bytes[1] == 0x01 && bytes[2] == 0x0d && bytes[3] == 0xb8 {
			return false
		}
	}
	return true
}

func mailHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func readMailGreeting(ctx context.Context, connection net.Conn) (string, string, *int) {
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	reader := bufio.NewReaderSize(io.LimitReader(connection, 4097), 4097)
	total, expected := 0, 0
	for range 20 {
		line, err := reader.ReadString('\n')
		total += len(line)
		if total > 4096 {
			return "unknown", "greeting_too_large", nil
		}
		if err != nil {
			if ctx.Err() != nil {
				status, code := mailContextStatus(ctx)
				return status, code, nil
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				return "timeout", "timeout", nil
			}
			return "unknown", "invalid_greeting", nil
		}
		if len(line) < 6 || !strings.HasSuffix(line, "\r\n") || (line[3] != ' ' && line[3] != '-') {
			return "unknown", "invalid_greeting", nil
		}
		code, err := strconv.Atoi(line[:3])
		if err != nil || code < 100 || code > 599 || expected != 0 && expected != code {
			return "unknown", "invalid_greeting", nil
		}
		expected = code
		if line[3] == '-' {
			continue
		}
		_, _ = io.WriteString(connection, "QUIT\r\n")
		if code == 220 {
			return "reachable", "", &code
		}
		if code >= 400 {
			return "refused", "", &code
		}
		return "unknown", "unexpected_greeting", &code
	}
	return "unknown", "greeting_too_many_lines", nil
}

func collectPTR(ctx context.Context, ip string) []string {
	return collectPTRWithResolver(ctx, ip, net.DefaultResolver)
}

func collectPTRWithResolver(ctx context.Context, ip string, resolver mailResolver) []string {
	names := make([]string, 0, 4)
	if _, err := netip.ParseAddr(ip); err != nil {
		return names
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values, err := resolver.LookupAddr(lookupCtx, ip)
	if err != nil {
		return names
	}
	seen, size := make(map[string]bool), 0
	for _, value := range values {
		name := strings.ToLower(strings.TrimSuffix(value, "."))
		if !mailHostname(name) || seen[name] {
			continue
		}
		if len(names) == 4 || size+len(name) > 1024 {
			break
		}
		names = append(names, name)
		seen[name] = true
		size += len(name)
	}
	return names
}
