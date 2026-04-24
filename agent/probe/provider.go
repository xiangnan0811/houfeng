package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type Provider struct {
	lastRun map[string]time.Time
	tcpRun  func(context.Context, agentapi.ProbeAssignment) (int, error)
	httpRun func(context.Context, agentapi.ProbeAssignment) (int, int, error)
	tlsRun  func(context.Context, agentapi.ProbeAssignment) (int, int, error)
}

type tcpConfig struct {
	Port int `json:"port"`
}

type httpConfig struct {
	Scheme              string `json:"scheme"`
	Path                string `json:"path"`
	Method              string `json:"method"`
	ExpectedStatusRange []int  `json:"expected_status_range"`
}

type tlsConfig struct {
	Port              int `json:"port"`
	ExpiryWarningDays int `json:"expiry_warning_days"`
}

type tlsHandshakeMarker interface {
	IsTLSHandshakeError() bool
}

type tlsHandshakeError struct {
	err error
}

func (e tlsHandshakeError) Error() string {
	return e.err.Error()
}

func (e tlsHandshakeError) Unwrap() error {
	return e.err
}

func (e tlsHandshakeError) IsTLSHandshakeError() bool {
	return true
}

func New() *Provider {
	return NewWithDeps(runTCPProbe, runHTTPProbe, runTLSProbe)
}

func NewWithDeps(
	tcpRun func(context.Context, agentapi.ProbeAssignment) (int, error),
	httpRun func(context.Context, agentapi.ProbeAssignment) (int, int, error),
	tlsRun func(context.Context, agentapi.ProbeAssignment) (int, int, error),
) *Provider {
	if tcpRun == nil {
		tcpRun = runTCPProbe
	}
	if httpRun == nil {
		httpRun = runHTTPProbe
	}
	if tlsRun == nil {
		tlsRun = runTLSProbe
	}
	return &Provider{
		lastRun: make(map[string]time.Time),
		tcpRun:  tcpRun,
		httpRun: httpRun,
		tlsRun:  tlsRun,
	}
}

func (p *Provider) CollectDue(ctx context.Context, plan *agentapi.SyncPlan, observedAt time.Time) ([]agentapi.ProbeObservationPayload, error) {
	if plan == nil || len(plan.ProbeAssignments) == 0 {
		return nil, nil
	}

	observations := make([]agentapi.ProbeObservationPayload, 0)
	for _, assignment := range plan.ProbeAssignments {
		duration, ok := frequencyTierDuration(assignment.FrequencyTier)
		if !ok {
			continue
		}
		if lastRun, ok := p.lastRun[assignment.ProbeItemID]; ok && observedAt.Before(lastRun.Add(duration)) {
			continue
		}

		observation, err := p.collectAssignment(ctx, assignment, observedAt)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
		p.lastRun[assignment.ProbeItemID] = observedAt
	}

	return observations, nil
}

func (p *Provider) collectAssignment(ctx context.Context, assignment agentapi.ProbeAssignment, observedAt time.Time) (agentapi.ProbeObservationPayload, error) {
	base := agentapi.ProbeObservationPayload{
		TargetID:           assignment.TargetID,
		ProbeItemID:        assignment.ProbeItemID,
		ProbeKind:          assignment.ProbeKind,
		ObservedAt:         observedAt,
		MaintenanceContext: assignment.MaintenanceContext,
	}

	timeout := time.Duration(assignment.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch assignment.ProbeKind {
	case agentapi.ProbeKindTCP:
		latencyMS, err := p.tcpRun(probeCtx, assignment)
		if err != nil {
			return failureObservation(base, classifyProbeError(agentapi.ProbeKindTCP, err), err.Error()), nil
		}
		base.ResultKind = agentapi.ProbeResultSuccess
		base.LatencyMS = intPtr(latencyMS)
		return base, nil
	case agentapi.ProbeKindHTTP:
		latencyMS, statusCode, err := p.httpRun(probeCtx, assignment)
		if err != nil {
			return failureObservation(base, classifyProbeError(agentapi.ProbeKindHTTP, err), err.Error()), nil
		}
		base.LatencyMS = intPtr(latencyMS)
		base.HTTPStatus = intPtr(statusCode)
		cfg, err := decodeHTTPConfig(assignment.Config)
		if err != nil {
			return agentapi.ProbeObservationPayload{}, err
		}
		if !statusWithinRange(statusCode, cfg.ExpectedStatusRange) {
			return failureObservation(base, agentapi.ProbeErrorHTTPStatus, fmt.Sprintf("unexpected http status %d", statusCode)), nil
		}
		base.ResultKind = agentapi.ProbeResultSuccess
		return base, nil
	case agentapi.ProbeKindTLS:
		latencyMS, expiryDays, err := p.tlsRun(probeCtx, assignment)
		if err != nil {
			return failureObservation(base, classifyProbeError(agentapi.ProbeKindTLS, err), err.Error()), nil
		}
		base.ResultKind = agentapi.ProbeResultSuccess
		base.LatencyMS = intPtr(latencyMS)
		base.TLSExpiryDays = intPtr(expiryDays)
		return base, nil
	default:
		return agentapi.ProbeObservationPayload{}, fmt.Errorf("unsupported probe kind %q", assignment.ProbeKind)
	}
}

func runTCPProbe(ctx context.Context, assignment agentapi.ProbeAssignment) (int, error) {
	cfg, err := decodeTCPConfig(assignment.Config)
	if err != nil {
		return 0, err
	}
	port := resolvePort(cfg.Port, assignment.TargetBasePort)
	if port <= 0 {
		return 0, fmt.Errorf("tcp probe missing port")
	}
	address := net.JoinHostPort(assignment.TargetHost, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, timeoutFromContext(ctx))
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return durationMilliseconds(start, time.Now()), nil
}

func runHTTPProbe(ctx context.Context, assignment agentapi.ProbeAssignment) (int, int, error) {
	cfg, err := decodeHTTPConfig(assignment.Config)
	if err != nil {
		return 0, 0, err
	}
	port := resolveHTTPPort(cfg.Scheme, assignment.TargetBasePort)
	url := fmt.Sprintf("%s://%s%s", cfg.Scheme, net.JoinHostPort(assignment.TargetHost, strconv.Itoa(port)), cfg.Path)
	request, err := http.NewRequestWithContext(ctx, cfg.Method, url, nil)
	if err != nil {
		return 0, 0, err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: assignment.TargetHost}}}
	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()
	return durationMilliseconds(start, time.Now()), response.StatusCode, nil
}

func runTLSProbe(ctx context.Context, assignment agentapi.ProbeAssignment) (int, int, error) {
	cfg, err := decodeTLSConfig(assignment.Config)
	if err != nil {
		return 0, 0, err
	}
	port := resolvePort(cfg.Port, assignment.TargetBasePort)
	if port <= 0 {
		return 0, 0, fmt.Errorf("tls probe missing port")
	}
	address := net.JoinHostPort(assignment.TargetHost, strconv.Itoa(port))
	start := time.Now()
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeoutFromContext(ctx)},
		Config:    &tls.Config{ServerName: assignment.TargetHost},
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if !isTLSConnectError(err) && !isTimeoutError(err) {
			err = tlsHandshakeError{err: err}
		}
		return 0, 0, err
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected tls connection type %T", conn)
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return 0, 0, tlsHandshakeError{err: fmt.Errorf("no peer certificate presented")}
	}
	expiryDays := int(time.Until(state.PeerCertificates[0].NotAfter).Hours() / 24)
	return durationMilliseconds(start, time.Now()), expiryDays, nil
}

func decodeTCPConfig(raw json.RawMessage) (tcpConfig, error) {
	var cfg tcpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return tcpConfig{}, fmt.Errorf("decode tcp config: %w", err)
	}
	return cfg, nil
}

func decodeHTTPConfig(raw json.RawMessage) (httpConfig, error) {
	var cfg httpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return httpConfig{}, fmt.Errorf("decode http config: %w", err)
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	return cfg, nil
}

func decodeTLSConfig(raw json.RawMessage) (tlsConfig, error) {
	var cfg tlsConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return tlsConfig{}, fmt.Errorf("decode tls config: %w", err)
	}
	return cfg, nil
}

func frequencyTierDuration(tier string) (time.Duration, bool) {
	switch tier {
	case agentapi.FrequencyTier1m:
		return time.Minute, true
	case agentapi.FrequencyTier5m:
		return 5 * time.Minute, true
	case agentapi.FrequencyTier15m:
		return 15 * time.Minute, true
	case agentapi.FrequencyTier6h:
		return 6 * time.Hour, true
	default:
		return 0, false
	}
}

func resolvePort(configPort int, basePort *int) int {
	if configPort > 0 {
		return configPort
	}
	if basePort != nil {
		return *basePort
	}
	return 0
}

func resolveHTTPPort(scheme string, basePort *int) int {
	if basePort != nil {
		return *basePort
	}
	if scheme == "https" {
		return 443
	}
	return 80
}

func statusWithinRange(statusCode int, expected []int) bool {
	if len(expected) != 2 {
		return true
	}
	return statusCode >= expected[0] && statusCode <= expected[1]
}

func classifyProbeError(kind string, err error) string {
	var netErr net.Error
	if err != nil && errors.As(err, &netErr) && netErr.Timeout() {
		return agentapi.ProbeErrorTimeout
	}
	if kind == agentapi.ProbeKindTLS {
		var marker tlsHandshakeMarker
		if errors.As(err, &marker) && marker.IsTLSHandshakeError() {
			return agentapi.ProbeErrorTLSHandshake
		}
	}
	return agentapi.ProbeErrorConnect
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return err != nil && errors.As(err, &netErr) && netErr.Timeout()
}

func isTLSConnectError(err error) bool {
	if err == nil {
		return false
	}
	var (
		dnsErr   *net.DNSError
		opErr    *net.OpError
		addrErr  *net.AddrError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		certInv  x509.CertificateInvalidError
		record   tls.RecordHeaderError
		certVer  *tls.CertificateVerificationError
	)
	switch {
	case errors.As(err, &dnsErr), errors.As(err, &addrErr):
		return true
	case errors.As(err, &opErr) && opErr.Op == "dial":
		return true
	case errors.As(err, &unknown), errors.As(err, &hostname), errors.As(err, &certInv), errors.As(err, &record), errors.As(err, &certVer):
		return false
	}
	return false
}

func timeoutFromContext(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout > 0 {
			return timeout
		}
		return time.Nanosecond
	}
	return 5 * time.Second
}

func failureObservation(base agentapi.ProbeObservationPayload, code, summary string) agentapi.ProbeObservationPayload {
	base.ResultKind = agentapi.ProbeResultFailure
	base.ErrorCode = code
	base.ErrorSummary = summary
	return base
}

func intPtr(value int) *int { return &value }

func durationMilliseconds(start, end time.Time) int {
	elapsed := end.Sub(start).Milliseconds()
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed)
}
