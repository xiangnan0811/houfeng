package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordclientcleanup"
	"houfeng/internal/center/store"
)

type cleanupProtocolRepository struct {
	events        []recordclientcleanup.Event
	acked         atomic.Int32
	unknownCursor bool
	reconcile     func(context.Context, recordauth.ActorScope, recordclientcleanup.ReconcileRequest) (recordclientcleanup.ReconcileResult, error)
}

func (repository *cleanupProtocolRepository) Reconcile(ctx context.Context, actor recordauth.ActorScope, request recordclientcleanup.ReconcileRequest) (recordclientcleanup.ReconcileResult, error) {
	if repository.reconcile == nil {
		return recordclientcleanup.ReconcileResult{}, errors.New("unexpected reconciliation")
	}
	return repository.reconcile(ctx, actor, request)
}
func (repository *cleanupProtocolRepository) ValidateCursor(context.Context, recordauth.ActorScope, string, string) error {
	if repository.unknownCursor {
		return recordclientcleanup.ErrReconcileRequired
	}
	return nil
}
func (repository *cleanupProtocolRepository) Next(context.Context, recordauth.ActorScope, string) (*recordclientcleanup.Event, error) {
	index := int(repository.acked.Load())
	if index >= len(repository.events) {
		return nil, nil
	}
	event := repository.events[index]
	return &event, nil
}
func (repository *cleanupProtocolRepository) Acknowledge(_ context.Context, _ recordauth.ActorScope, _ string, ack recordclientcleanup.Acknowledgement) error {
	index := int(repository.acked.Load())
	if index >= len(repository.events) || ack != repository.events[index].Acknowledgement() {
		return recordclientcleanup.ErrInvalidAcknowledgement
	}
	repository.acked.Add(1)
	return nil
}

type cleanupSessionValidator struct{ expired atomic.Bool }

func (validator *cleanupSessionValidator) ValidateSession(context.Context, string) error {
	if validator.expired.Load() {
		return auth.ErrSessionExpired
	}
	return nil
}

func cleanupProtocolServer(t *testing.T, repository *cleanupProtocolRepository, validator *cleanupSessionValidator) *httptest.Server {
	t.Helper()
	handler := handlers.RecordClientCleanupStream(repository, validator)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := sessionctx.WithSessionID(r.Context(), "cleanup-session")
		ctx = sessionctx.WithActorScope(ctx, recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	return server
}
func cleanupProtocolEvent(cursor, recordID string) recordclientcleanup.Event {
	return recordclientcleanup.Event{Version: 1, Cursor: cursor, OperationID: "rpo_cleanup", RecordID: recordID, CleanupEpoch: "9007199254740995", Scopes: []string{"markdown_workspace", "comparison_save_attempt"}}
}
func readCleanupProtocolEvent(t *testing.T, ctx context.Context, conn *websocket.Conn) recordclientcleanup.Event {
	t.Helper()
	var message struct {
		Type  string                    `json:"type"`
		Event recordclientcleanup.Event `json:"event"`
	}
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "cleanup" {
		t.Fatalf("unexpected message type %q", message.Type)
	}
	return message.Event
}

func TestRecordClientCleanupReconcileAllocationWireContract(t *testing.T) {
	const recordID = "rec2_0123456789abcdef0123456789abcdef"
	const allocationID = "rqa_0123456789abcdef0123456789abcdef"
	var received recordclientcleanup.ReconcileRequest
	repository := &cleanupProtocolRepository{reconcile: func(_ context.Context, _ recordauth.ActorScope, request recordclientcleanup.ReconcileRequest) (recordclientcleanup.ReconcileResult, error) {
		received = request
		return recordclientcleanup.ReconcileResult{Allowed: []string{}, Pending: []string{recordID}, Invalidate: []string{}}, nil
	}}
	handler := handlers.RecordClientCleanupReconcile(repository)
	actor := recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/records/client-cleanup/reconcile", strings.NewReader(body))
		request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	body := `{"browser_id":"browser_a","record_ids":[],"allocation_references":[{"record_id":"` + recordID + `","allocation_id":"` + allocationID + `"}]}`
	response := post(body)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s", response.Code, response.Body.String())
	}
	if len(received.AllocationReferences) != 1 || received.AllocationReferences[0] != (recordclientcleanup.AllocationReference{RecordID: recordID, AllocationID: allocationID}) {
		t.Fatalf("reconcile request = %#v", received)
	}
	var result recordclientcleanup.ReconcileResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Allowed) != 0 || len(result.Pending) != 1 || result.Pending[0] != recordID || len(result.Invalidate) != 0 {
		t.Fatalf("reconcile response = %#v, %v", result, err)
	}
	contradictory := `{"browser_id":"browser_a","record_ids":["` + recordID + `"],"allocation_references":[{"record_id":"` + recordID + `","allocation_id":"` + allocationID + `"}]}`
	if response := post(contradictory); response.Code != http.StatusBadRequest {
		t.Fatalf("contradictory request status=%d body=%s", response.Code, response.Body.String())
	}
	repository.reconcile = func(context.Context, recordauth.ActorScope, recordclientcleanup.ReconcileRequest) (recordclientcleanup.ReconcileResult, error) {
		return recordclientcleanup.ReconcileResult{}, store.ErrRecordSubjectUnavailable
	}
	if response := post(body); response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), `"invalidate"`) {
		t.Fatalf("unavailable status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRecordClientCleanupStreamExactAcknowledgementAndReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repository := &cleanupProtocolRepository{events: []recordclientcleanup.Event{cleanupProtocolEvent("9007199254740993", "rec_first"), cleanupProtocolEvent("9007199254740994", "rec_second")}}
	server := cleanupProtocolServer(t, repository, &cleanupSessionValidator{})
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "?browser_id=browser_a"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := readCleanupProtocolEvent(t, ctx, conn)
	if first.Cursor != "9007199254740993" || first.CleanupEpoch != "9007199254740995" {
		t.Fatalf("lossy wire identity: %+v", first)
	}
	conn.CloseNow()
	// A receive without a durable acknowledgement is replayed on reconnect.
	conn, _, err = websocket.Dial(ctx, url+"&cursor="+first.Cursor, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	replayed := readCleanupProtocolEvent(t, ctx, conn)
	if replayed.Acknowledgement() != first.Acknowledgement() {
		t.Fatal("unacknowledged event skipped")
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "ack", "ack": first.Acknowledgement()}); err != nil {
		t.Fatal(err)
	}
	second := readCleanupProtocolEvent(t, ctx, conn)
	if second.RecordID != "rec_second" || repository.acked.Load() != 1 {
		t.Fatal("ordered advancement failed")
	}
	wrong := second.Acknowledgement()
	wrong.CleanupEpoch = "9007199254740996"
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "ack", "ack": wrong}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("wrong event identity was accepted")
	}
	if repository.acked.Load() != 1 {
		t.Fatal("wrong acknowledgement became a receipt")
	}
}

func TestRecordClientCleanupStreamRechecksSessionBeforeAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repository := &cleanupProtocolRepository{events: []recordclientcleanup.Event{cleanupProtocolEvent("1", "rec_first")}}
	validator := &cleanupSessionValidator{}
	server := cleanupProtocolServer(t, repository, validator)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"?browser_id=browser_a", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	event := readCleanupProtocolEvent(t, ctx, conn)
	validator.expired.Store(true)
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "ack", "ack": event.Acknowledgement()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("expired session remained open")
	}
	if repository.acked.Load() != 0 {
		t.Fatal("expired session wrote a receipt")
	}
}

func TestRecordClientCleanupStreamKeepsIdleConnectionAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	repository := &cleanupProtocolRepository{}
	server := cleanupProtocolServer(t, repository, &cleanupSessionValidator{})
	pings := make(chan struct{}, 1)
	options := &websocket.DialOptions{
		OnPingReceived: func(context.Context, []byte) bool {
			select {
			case pings <- struct{}{}:
			default:
			}
			return true
		},
	}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"?browser_id=browser_a", options)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	// CloseRead keeps control frames flowing and rejects unexpected application frames.
	select {
	case <-pings:
	case <-conn.CloseRead(ctx).Done():
		t.Fatal("idle stream did not send a WebSocket ping before the deadline")
	}
	if repository.acked.Load() != 0 {
		t.Fatal("a transport ping acknowledged a cleanup event")
	}
}

func TestRecordClientCleanupStreamUnknownCursorAndForeignOrigin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := cleanupProtocolServer(t, &cleanupProtocolRepository{unknownCursor: true}, &cleanupSessionValidator{})
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "?browser_id=browser_a&cursor=99"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var message struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "reconcile_required" {
		t.Fatalf("unknown cursor silently accepted: %+v", message)
	}
	foreign, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://untrusted.example"}}})
	if foreign != nil {
		foreign.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin accepted: status=%v error=%v", response, err)
	}
}
