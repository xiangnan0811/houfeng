package ipquality

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type mailTestResolver struct {
	mx  func(context.Context, string) ([]*net.MX, error)
	ips func(context.Context, string, string) ([]net.IP, error)
	ptr func(context.Context, string) ([]string, error)
}

func (r mailTestResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return r.mx(ctx, name)
}
func (r mailTestResolver) LookupIP(ctx context.Context, family, name string) ([]net.IP, error) {
	return r.ips(ctx, family, name)
}
func (r mailTestResolver) LookupAddr(ctx context.Context, name string) ([]string, error) {
	return r.ptr(ctx, name)
}
func mailResultFixture() agentapi.IPQualityMailResult {
	return agentapi.IPQualityMailResult{Provider: "gmail", Domain: "gmail.com", ObservedAt: time.Unix(100, 0).UTC()}
}
func mailResolverFixture() mailTestResolver {
	return mailTestResolver{
		mx: func(context.Context, string) ([]*net.MX, error) {
			return []*net.MX{{Host: "mx.example.", Pref: 10}}, nil
		},
		ips: func(_ context.Context, family, _ string) ([]net.IP, error) {
			ip := "8.8.8.8"
			if family == "ip6" {
				ip = "2606:4700:4700::1111"
			}
			return []net.IP{net.ParseIP(ip)}, nil
		},
	}
}

// A real socket fixture reads everything sent until the probe closes. No SMTP
// message commands are expected, and the server never receives the banner back.
func mailSMTPFixture(t *testing.T, network, greeting string) (string, <-chan string) {
	t.Helper()
	address := "127.0.0.1:0"
	if network == "tcp6" {
		address = "[::1]:0"
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	commands := make(chan string, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			commands <- "accept_failed"
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		if greeting != "" {
			_, _ = io.WriteString(connection, greeting)
		}
		body, _ := io.ReadAll(connection)
		commands <- string(body)
	}()
	return listener.Addr().String(), commands
}

func TestMailRealTCPFamilyGreetingAndQUITOnly(t *testing.T) {
	for _, family := range []int{4, 6} {
		for _, test := range []struct {
			name, greeting, status string
			code                   int
		}{
			{"multiline", "220-first line\r\n220-second line\r\n220 ready\r\n", "reachable", 220},
			{"temporary_refusal", "450 temporarily unavailable\r\n", "refused", 450},
			{"permanent_refusal", "550 denied\r\n", "refused", 550},
			{"unexpected", "250 ready\r\n", "unknown", 250},
			{"timeout", "", "timeout", 0},
		} {
			t.Run(fmt.Sprintf("%d/%s", family, test.name), func(t *testing.T) {
				network := fmt.Sprintf("tcp%d", family)
				address, commands := mailSMTPFixture(t, network, test.greeting)
				calls := 0
				probe := mailProbe{resolver: mailResolverFixture(), dial: func(ctx context.Context, gotNetwork, target string) (net.Conn, error) {
					calls++
					if gotNetwork != network || !strings.HasSuffix(target, ":25") {
						t.Fatalf("wrong family/port: %s %s", gotNetwork, target)
					}
					return (&net.Dialer{}).DialContext(ctx, network, address)
				}}
				ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
				defer cancel()
				result := probe.provider(ctx, mailResultFixture(), family)
				if result.Status != test.status || calls != 1 || result.LatencyMS == nil || result.MXHost != "mx.example" {
					t.Fatalf("mail result: %+v calls %d", result, calls)
				}
				if test.code != 0 && (result.GreetingCode == nil || *result.GreetingCode != test.code) {
					t.Fatalf("greeting: %+v", result)
				}
				if test.code == 0 && result.GreetingCode != nil {
					t.Fatalf("fabricated greeting: %+v", result)
				}
				want := "QUIT\r\n"
				if test.code == 0 {
					want = ""
				}
				select {
				case got := <-commands:
					if got != want {
						t.Fatalf("SMTP commands = %q, want %q", got, want)
					}
				case <-time.After(time.Second):
					t.Fatal("socket remained open")
				}
			})
		}
	}
}

func TestMailIPv6SkipsMXWithoutAAAAAndConnectsNext(t *testing.T) {
	address, commands := mailSMTPFixture(t, "tcp6", "220 ready\r\n")
	resolver := mailResolverFixture()
	resolver.mx = func(context.Context, string) ([]*net.MX, error) {
		return []*net.MX{{Host: "later.example.", Pref: 20}, {Host: "first.example.", Pref: 10}}, nil
	}
	lookups, calls := []string{}, 0
	resolver.ips = func(_ context.Context, family, host string) ([]net.IP, error) {
		if family != "ip6" {
			t.Fatalf("family fallback: %s", family)
		}
		lookups = append(lookups, host)
		if host == "first.example" {
			return nil, nil
		}
		return []net.IP{net.ParseIP("2606:4700:4700::1111")}, nil
	}
	probe := mailProbe{resolver: resolver, dial: func(ctx context.Context, network, target string) (net.Conn, error) {
		calls++
		if network != "tcp6" || target != "[2606:4700:4700::1111]:25" {
			t.Fatalf("wrong target %s %s", network, target)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := probe.provider(ctx, mailResultFixture(), 6)
	if result.Status != "reachable" || result.MXHost != "later.example" || calls != 1 || strings.Join(lookups, ",") != "first.example,later.example" {
		t.Fatalf("next MX: %+v %v calls %d", result, lookups, calls)
	}
	if got := <-commands; got != "QUIT\r\n" {
		t.Fatalf("commands: %q", got)
	}
}

func TestMailDNSAndCandidateBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, status, code string
		mxs                []*net.MX
		mxErr              error
		ips                []net.IP
		ipErr              error
		family             int
	}{
		{"null_mx", "no_mx", "", []*net.MX{{Host: ".", Pref: 0}}, nil, nil, nil, 4},
		{"mx_dns_failure", "unknown", "dns_failed", nil, &net.DNSError{Err: "servfail"}, nil, nil, 4},
		{"no_ipv6", "no_address", "", []*net.MX{{Host: "mx.example"}}, nil, []net.IP{net.ParseIP("8.8.8.8")}, nil, 6},
		{"private4", "unknown", "non_public_target", []*net.MX{{Host: "mx.example"}}, nil, []net.IP{net.ParseIP("10.0.0.1")}, nil, 4},
		{"metadata", "unknown", "non_public_target", []*net.MX{{Host: "mx.example"}}, nil, []net.IP{net.ParseIP("169.254.169.254")}, nil, 4},
		{"loopback", "unknown", "non_public_target", []*net.MX{{Host: "mx.example"}}, nil, []net.IP{net.ParseIP("127.0.0.1")}, nil, 4},
		{"private6", "unknown", "non_public_target", []*net.MX{{Host: "mx.example"}}, nil, []net.IP{net.ParseIP("fd00::1")}, nil, 6},
		{"mx_address_dns_failure", "unknown", "dns_failed", []*net.MX{{Host: "mx.example"}}, nil, nil, &net.DNSError{Err: "servfail"}, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := mailTestResolver{mx: func(context.Context, string) ([]*net.MX, error) { return test.mxs, test.mxErr }, ips: func(context.Context, string, string) ([]net.IP, error) { return test.ips, test.ipErr }}
			probe := mailProbe{resolver: resolver, dial: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("unsafe/unavailable target dialed")
				return nil, nil
			}}
			result := probe.provider(context.Background(), mailResultFixture(), test.family)
			if result.Status != test.status || result.ErrorCode != test.code || result.TargetIP != "" {
				t.Fatalf("result: %+v", result)
			}
		})
	}
}

func TestMailCandidateLimitAndDeduplication(t *testing.T) {
	resolver := mailResolverFixture()
	resolver.mx = func(context.Context, string) ([]*net.MX, error) {
		mxs := make([]*net.MX, 0, 22)
		for i := range 21 {
			mxs = append(mxs, &net.MX{Host: fmt.Sprintf("mx%02d.example.", i), Pref: uint16(i)})
		}
		return append(mxs, &net.MX{Host: "MX00.EXAMPLE.", Pref: 5}), nil
	}
	lookups := 0
	resolver.ips = func(context.Context, string, string) ([]net.IP, error) { lookups++; return nil, nil }
	result := (mailProbe{resolver: resolver}).provider(context.Background(), mailResultFixture(), 6)
	if lookups != 20 || result.Status != "unknown" || result.ErrorCode != "mx_candidates_truncated" {
		t.Fatalf("truncated list became clean: %+v calls %d", result, lookups)
	}
}

func TestMailFallbackOnlyOnAbsentMX(t *testing.T) {
	for _, notFound := range []bool{false, true} {
		resolver := mailResolverFixture()
		resolver.mx = func(context.Context, string) ([]*net.MX, error) {
			if notFound {
				return nil, &net.DNSError{IsNotFound: true}
			}
			return nil, nil
		}
		resolver.ips = func(_ context.Context, _, host string) ([]net.IP, error) {
			if host != "gmail.com" {
				t.Fatalf("fallback host: %s", host)
			}
			return nil, nil
		}
		result := (mailProbe{resolver: resolver}).provider(context.Background(), mailResultFixture(), 4)
		if result.Status != "no_address" {
			t.Fatalf("fallback result: %+v", result)
		}
	}
}

func TestMailAtMostTwoTCPAttemptsAndNoRetryAfterGreeting(t *testing.T) {
	resolver := mailResolverFixture()
	resolver.mx = func(context.Context, string) ([]*net.MX, error) {
		return []*net.MX{{Host: "one.example", Pref: 1}, {Host: "two.example", Pref: 2}, {Host: "three.example", Pref: 3}}, nil
	}
	calls := 0
	probe := mailProbe{resolver: resolver, dial: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, &net.OpError{Op: "dial", Net: "tcp4", Err: syscall.ECONNREFUSED}
	}}
	result := probe.provider(context.Background(), mailResultFixture(), 4)
	if result.Status != "unknown" || result.ErrorCode != "connection_refused" || result.GreetingCode != nil || calls != 2 || result.MXHost != "two.example" {
		t.Fatalf("connection budget: %+v calls %d", result, calls)
	}
	address, commands := mailSMTPFixture(t, "tcp4", "220 ready\r\n")
	calls = 0
	probe.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		calls++
		if calls == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp4", Err: syscall.ECONNREFUSED}
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result = probe.provider(ctx, mailResultFixture(), 4)
	if result.Status != "reachable" || result.ErrorCode != "" || result.GreetingCode == nil || *result.GreetingCode != 220 || calls != 2 || result.MXHost != "two.example" {
		t.Fatalf("TCP refusal prevented next MX: %+v calls %d", result, calls)
	}
	if got := <-commands; got != "QUIT\r\n" {
		t.Fatalf("commands: %q", got)
	}
	address, commands = mailSMTPFixture(t, "tcp4", "550 denied\r\n")
	calls = 0
	probe.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		calls++
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result = probe.provider(ctx, mailResultFixture(), 4)
	if result.Status != "refused" || result.ErrorCode != "" || result.GreetingCode == nil || *result.GreetingCode != 550 || calls != 1 {
		t.Fatalf("greeting caused retry: %+v calls %d", result, calls)
	}
	if got := <-commands; got != "QUIT\r\n" {
		t.Fatalf("commands: %q", got)
	}
}

func TestMailMalformedAndBoundedGreetings(t *testing.T) {
	for _, greeting := range []string{"220-first\r\n550 last\r\n", "220 ready\n", strings.Repeat("220-more\r\n", 20), "220 " + strings.Repeat("a", 4096) + "\r\n"} {
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); defer server.Close(); _, _ = io.WriteString(server, greeting) }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		status, code, value := readMailGreeting(ctx, client)
		cancel()
		<-done
		if status != "unknown" || code == "" || value != nil {
			t.Fatalf("invalid banner accepted: %s %s %v", status, code, value)
		}
	}
}

func TestMailCancellationClosesRealSocket(t *testing.T) {
	address, commands := mailSMTPFixture(t, "tcp4", "")
	connected := make(chan struct{})
	probe := mailProbe{resolver: mailResolverFixture(), dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
		close(connected)
		return connection, err
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan agentapi.IPQualityMailResult, 1)
	go func() { done <- probe.provider(ctx, mailResultFixture(), 4) }()
	<-connected
	cancel()
	select {
	case result := <-done:
		if result.Status != "unknown" || result.ErrorCode != "canceled" {
			t.Fatalf("cancellation: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not join")
	}
	select {
	case got := <-commands:
		if got != "" {
			t.Fatalf("unexpected SMTP command: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled socket remained open")
	}
}

func TestMailSharedSixSlotsAndCanceledQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var active, peak atomic.Int32
		resolver := mailResolverFixture()
		resolver.mx = func(ctx context.Context, _ string) ([]*net.MX, error) {
			count := active.Add(1)
			for {
				old := peak.Load()
				if count <= old || peak.CompareAndSwap(old, count) {
					break
				}
			}
			defer active.Add(-1)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		probe := mailProbe{resolver: resolver}
		slots := make(chan struct{}, 6)
		var workers sync.WaitGroup
		workers.Add(2)
		for _, family := range []int{4, 6} {
			go func() {
				defer workers.Done()
				ip := "8.8.8.8"
				if family == 6 {
					ip = "2606:4700:4700::1111"
				}
				results := probe.collect(ctx, ip, family, time.Unix(100, 0), slots)
				if len(results) != 12 {
					t.Errorf("registry size: %d", len(results))
				}
				for _, result := range results {
					if result.ErrorCode != "canceled" {
						t.Errorf("queue cancellation: %+v", result)
					}
				}
			}()
		}
		synctest.Wait()
		if active.Load() != 6 || peak.Load() != 6 {
			t.Fatalf("shared slots: active %d peak %d", active.Load(), peak.Load())
		}
		cancel()
		workers.Wait()
		if active.Load() != 0 || len(slots) != 0 {
			t.Fatal("workers/slots leaked after cancellation")
		}
	})
}

func TestMailProviderBudgetStartsAfterSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resolver := mailResolverFixture()
		resolver.mx = func(ctx context.Context, _ string) ([]*net.MX, error) { <-ctx.Done(); return nil, ctx.Err() }
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan []agentapi.IPQualityMailResult, 1)
		go func() { done <- (mailProbe{resolver: resolver}).collect(ctx, "8.8.8.8", 4, time.Unix(100, 0), slots) }()
		time.Sleep(10 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("queue time exhausted child budgets")
		default:
		}
		<-slots
		time.Sleep(5 * time.Second)
		synctest.Wait()
		cancel()
		results := <-done
		if len(results) != 12 {
			t.Fatal(results)
		}
		found := false
		for _, result := range results {
			if result.Status == "timeout" {
				found = true
			}
		}
		if !found {
			t.Fatal("admitted probe did not observe five-second budget")
		}
	})
}

func TestPTRUsesSelectedAddressAndBoundsNames(t *testing.T) {
	resolver := mailTestResolver{ptr: func(_ context.Context, ip string) ([]string, error) {
		if ip != "2606:4700:4700::1111" {
			t.Fatalf("PTR queried other family: %s", ip)
		}
		return []string{"FIRST.EXAMPLE.", "first.example", "bad name", "second.example.", "third.example", "fourth.example", "fifth.example"}, nil
	}}
	names := collectPTRWithResolver(context.Background(), "2606:4700:4700::1111", resolver)
	if strings.Join(names, ",") != "first.example,second.example,third.example,fourth.example" {
		t.Fatalf("bounded PTR: %v", names)
	}
	resolver.ptr = func(context.Context, string) ([]string, error) { return nil, errors.New("private DNS text") }
	if names := collectPTRWithResolver(context.Background(), "8.8.8.8", resolver); names == nil || len(names) != 0 {
		t.Fatalf("PTR failure: %v", names)
	}
}

func TestMailGreetingTimeoutNeverTriesSecondMX(t *testing.T) {
	for _, family := range []int{4, 6} {
		network := fmt.Sprintf("tcp%d", family)
		address, commands := mailSMTPFixture(t, network, "")
		resolver := mailResolverFixture()
		resolver.mx = func(context.Context, string) ([]*net.MX, error) {
			return []*net.MX{{Host: "one.example", Pref: 1}, {Host: "two.example", Pref: 2}}, nil
		}
		calls := 0
		probe := mailProbe{resolver: resolver, dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			calls++
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		result := probe.provider(ctx, mailResultFixture(), family)
		cancel()
		if result.Status != "timeout" || result.ErrorCode != "timeout" || calls != 1 || result.MXHost != "one.example" {
			t.Fatalf("greeting timeout retried: %+v calls %d", result, calls)
		}
		select {
		case got := <-commands:
			if got != "" {
				t.Fatalf("unexpected commands: %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out socket not closed")
		}
	}
}

func TestMailTCP6CancellationClosesSocket(t *testing.T) {
	address, commands := mailSMTPFixture(t, "tcp6", "")
	connected := make(chan struct{})
	probe := mailProbe{resolver: mailResolverFixture(), dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		if network != "tcp6" {
			t.Fatalf("wrong network: %s", network)
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
		close(connected)
		return connection, err
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan agentapi.IPQualityMailResult, 1)
	go func() { done <- probe.provider(ctx, mailResultFixture(), 6) }()
	<-connected
	cancel()
	select {
	case result := <-done:
		if result.ErrorCode != "canceled" {
			t.Fatalf("canceled result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("IPv6 worker did not join")
	}
	select {
	case got := <-commands:
		if got != "" {
			t.Fatalf("unexpected commands: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("IPv6 socket remained open")
	}
}

func TestMailFixedRegistryAndSkippedTargets(t *testing.T) {
	resolver := mailTestResolver{mx: func(context.Context, string) ([]*net.MX, error) { return []*net.MX{{Host: ".", Pref: 0}}, nil }}
	probe := mailProbe{resolver: resolver, dial: func(context.Context, string, string) (net.Conn, error) { t.Fatal("null MX connected"); return nil, nil }}
	observed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("fixture", 3600))
	results := probe.collect(context.Background(), "8.8.8.8", 4, observed, make(chan struct{}, 6))
	if len(results) != 12 {
		t.Fatalf("registry size: %d", len(results))
	}
	for i, result := range results {
		if result.Provider != mailProviders[i].id || result.Domain != mailProviders[i].domain || result.Status != "no_mx" || !result.ObservedAt.Equal(observed) || result.ObservedAt.Location() != time.UTC || result.TargetIP != "" {
			t.Fatalf("registry result: %+v", result)
		}
	}
	for _, target := range []struct {
		ip     string
		family int
	}{{"", 4}, {"127.0.0.1", 4}, {"8.8.8.8", 6}, {"2606:4700:4700::1111", 4}} {
		rows := probe.collect(context.Background(), target.ip, target.family, observed, make(chan struct{}, 6))
		for _, row := range rows {
			if row.Status != "skipped" || row.ErrorCode != "invalid_target" {
				t.Fatalf("unsafe report target: %+v", row)
			}
		}
	}
	rows := skippedMailResults(observed, "egress_ambiguous")
	for _, row := range rows {
		if row.Status != "skipped" || row.ErrorCode != "egress_ambiguous" || row.MXHost != "" || row.TargetIP != "" {
			t.Fatalf("skipped diagnostics leaked facts: %+v", row)
		}
	}
}
