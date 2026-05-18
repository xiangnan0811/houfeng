package probe_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/agent/probe"
	"houfeng/internal/contracts/agentapi"
)

func TestCollectDueRunsTCPHTTPAndTLSAssignments(t *testing.T) {
	provider := probe.NewWithDeps(
		func(context.Context, agentapi.ProbeAssignment) (int, error) { return 12, nil },
		func(context.Context, agentapi.ProbeAssignment) (int, int, error) { return 34, 204, nil },
		func(context.Context, agentapi.ProbeAssignment) (int, int, error) { return 56, 7, nil },
	)
	observedAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	plan := &agentapi.SyncPlan{ProbeAssignments: []agentapi.ProbeAssignment{
		{TargetID: "tg_tcp", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"port":443}`)},
		{TargetID: "tg_http", ProbeItemID: "pb_http", ProbeKind: agentapi.ProbeKindHTTP, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"scheme":"https","path":"/healthz","method":"GET","expected_status_range":[200,299]}`)},
		{TargetID: "tg_tls", ProbeItemID: "pb_tls", ProbeKind: agentapi.ProbeKindTLS, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"port":443,"expiry_warning_days":30}`)},
	}}

	observations, err := provider.CollectDue(context.Background(), plan, observedAt)
	if err != nil {
		t.Fatalf("CollectDue() error = %v", err)
	}
	if len(observations) != 3 {
		t.Fatalf("len(observations) = %d, want 3", len(observations))
	}
	if observations[0].ResultKind != agentapi.ProbeResultSuccess || observations[0].LatencyMS == nil || *observations[0].LatencyMS != 12 {
		t.Fatalf("tcp observation = %#v, want success latency 12", observations[0])
	}
	if observations[1].HTTPStatus == nil || *observations[1].HTTPStatus != 204 {
		t.Fatalf("http observation = %#v, want status 204", observations[1])
	}
	if observations[2].TLSExpiryDays == nil || *observations[2].TLSExpiryDays != 7 {
		t.Fatalf("tls observation = %#v, want expiry 7", observations[2])
	}
}

func TestCollectDueMapsFailuresToFailureObservations(t *testing.T) {
	provider := probe.NewWithDeps(
		func(context.Context, agentapi.ProbeAssignment) (int, error) { return 0, timeoutErr{} },
		func(context.Context, agentapi.ProbeAssignment) (int, int, error) { return 15, 503, nil },
		func(context.Context, agentapi.ProbeAssignment) (int, int, error) {
			return 0, 0, tlsHandshakeErr{message: "bad certificate"}
		},
	)
	observedAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	plan := &agentapi.SyncPlan{ProbeAssignments: []agentapi.ProbeAssignment{
		{TargetID: "tg_tcp", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"port":443}`)},
		{TargetID: "tg_http", ProbeItemID: "pb_http", ProbeKind: agentapi.ProbeKindHTTP, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"scheme":"https","path":"/healthz","method":"GET","expected_status_range":[200,299]}`)},
		{TargetID: "tg_tls", ProbeItemID: "pb_tls", ProbeKind: agentapi.ProbeKindTLS, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"port":443,"expiry_warning_days":30}`)},
	}}

	observations, err := provider.CollectDue(context.Background(), plan, observedAt)
	if err != nil {
		t.Fatalf("CollectDue() error = %v", err)
	}
	if observations[0].ErrorCode != agentapi.ProbeErrorTimeout {
		t.Fatalf("tcp ErrorCode = %q, want %q", observations[0].ErrorCode, agentapi.ProbeErrorTimeout)
	}
	if observations[1].ErrorCode != agentapi.ProbeErrorHTTPStatus || observations[1].HTTPStatus == nil || *observations[1].HTTPStatus != 503 {
		t.Fatalf("http observation = %#v, want http_status failure", observations[1])
	}
	if observations[2].ErrorCode != agentapi.ProbeErrorTLSHandshake {
		t.Fatalf("tls ErrorCode = %q, want %q", observations[2].ErrorCode, agentapi.ProbeErrorTLSHandshake)
	}
}

func TestCollectDueMapsTLSConnectFailuresToConnect(t *testing.T) {
	provider := probe.NewWithDeps(
		nil,
		nil,
		func(context.Context, agentapi.ProbeAssignment) (int, int, error) {
			return 0, 0, errors.New("connection refused")
		},
	)
	observedAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	plan := &agentapi.SyncPlan{ProbeAssignments: []agentapi.ProbeAssignment{{
		TargetID: "tg_tls", ProbeItemID: "pb_tls", ProbeKind: agentapi.ProbeKindTLS, FrequencyTier: agentapi.FrequencyTier1m, TimeoutSeconds: 5, Config: []byte(`{"port":443,"expiry_warning_days":30}`),
	}}}

	observations, err := provider.CollectDue(context.Background(), plan, observedAt)
	if err != nil {
		t.Fatalf("CollectDue() error = %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("len(observations) = %d, want 1", len(observations))
	}
	if observations[0].ErrorCode != agentapi.ProbeErrorConnect {
		t.Fatalf("tls ErrorCode = %q, want %q", observations[0].ErrorCode, agentapi.ProbeErrorConnect)
	}
}

func TestCollectDueOnlyRunsAssignmentsWhenDue(t *testing.T) {
	calls := 0
	provider := probe.NewWithDeps(
		func(context.Context, agentapi.ProbeAssignment) (int, error) { calls++; return 1, nil },
		nil,
		nil,
	)
	plan := &agentapi.SyncPlan{ProbeAssignments: []agentapi.ProbeAssignment{{
		TargetID: "tg_tcp", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, FrequencyTier: agentapi.FrequencyTier5s, TimeoutSeconds: 5, Config: []byte(`{"port":443}`),
	}}}
	firstAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	observations, err := provider.CollectDue(context.Background(), plan, firstAt)
	if err != nil {
		t.Fatalf("first CollectDue() error = %v", err)
	}
	if len(observations) != 1 || calls != 1 {
		t.Fatalf("first run observations=%d calls=%d, want 1/1", len(observations), calls)
	}

	observations, err = provider.CollectDue(context.Background(), plan, firstAt.Add(4*time.Second))
	if err != nil {
		t.Fatalf("second CollectDue() error = %v", err)
	}
	if len(observations) != 0 || calls != 1 {
		t.Fatalf("not-due run observations=%d calls=%d, want 0/1", len(observations), calls)
	}

	observations, err = provider.CollectDue(context.Background(), plan, firstAt.Add(5*time.Second))
	if err != nil {
		t.Fatalf("third CollectDue() error = %v", err)
	}
	if len(observations) != 1 || calls != 2 {
		t.Fatalf("due run observations=%d calls=%d, want 1/2", len(observations), calls)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type tlsHandshakeErr struct{ message string }

func (e tlsHandshakeErr) Error() string {
	return e.message
}

func (tlsHandshakeErr) IsTLSHandshakeError() bool {
	return true
}
