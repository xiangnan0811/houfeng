package runtime_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/hostsample"
	"houfeng/agent/probe"
	agentruntime "houfeng/agent/runtime"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

// Labels are assertions-only fixture names. Wire and persisted batch identities
// always remain the producer UUID and allocated sequence assigned by FileStore.
var fixtureBatchLabels sync.Map

type runtimeTestStore struct {
	*syncqueue.FileStore
	session string
	opened  bool
}

func newRuntimeTestStore(t *testing.T, path string, opts syncqueue.Options) *runtimeTestStore {
	t.Helper()
	store := &runtimeTestStore{FileStore: syncqueue.NewFileStore(path, opts)}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func (s *runtimeTestStore) OpenSession(ctx context.Context, session string) error {
	if s.opened {
		if err := s.Close(); err != nil {
			return err
		}
	}
	if err := s.FileStore.OpenSession(ctx, session); err != nil {
		return err
	}
	s.session, s.opened = session, true
	return nil
}

func (s *runtimeTestStore) Close() error {
	s.opened = false
	return s.FileStore.Close()
}

// Fixture setup and post-Run inspection explicitly acquire their own lifetime
// lock. While Run owns the store these methods use that existing open session.
func (s *runtimeTestStore) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	if !s.opened {
		session := request.SessionID
		if session == "" {
			session = strings.SplitN(request.SyncToken, ".", 2)[0]
		}
		if err := s.OpenSession(ctx, session); err != nil {
			return "", err
		}
		defer s.Close()
	}
	if request.SessionID == "" {
		request.SessionID = s.session
	}
	label := ""
	if len(request.Heartbeats) > 0 {
		label = request.Heartbeats[0].SyncBatchID
	}
	id, err := s.FileStore.Enqueue(ctx, request)
	if err != nil {
		return "", err
	}
	entries, err := s.FileStore.List(ctx)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.ID == id && len(entry.Request.Heartbeats) > 0 && label != "" {
			fixtureBatchLabels.Store(entry.Request.Heartbeats[0].SyncBatchID, label)
		}
	}
	return id, nil
}

func (s *runtimeTestStore) List(ctx context.Context) ([]syncqueue.Entry, error) {
	if !s.opened {
		if err := s.OpenSession(ctx, s.session); err != nil {
			return nil, err
		}
		defer s.Close()
	}
	return s.FileStore.List(ctx)
}

func (q *fileQueueFaults) prepareTestQueue(t *testing.T) {
	if q.store == nil {
		q.store = newRuntimeTestStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{SkipFsync: true})
	}
}

func (q *cancelAfterSuccessfulDeleteQueue) prepareTestQueue(t *testing.T) {
	if preparer, ok := q.SyncQueue.(interface{ prepareTestQueue(*testing.T) }); ok {
		preparer.prepareTestQueue(t)
	}
}

func (q *fileQueueFaults) OpenSession(ctx context.Context, session string) error {
	if err := q.store.OpenSession(ctx, session); err != nil {
		return err
	}
	// Initial entries are fixture inputs only. Every runnable path thereafter
	// allocates, persists, lists, retires and reserves using the real FileStore.
	if q.entries != nil {
		existing, err := q.store.FileStore.List(ctx)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			for _, entry := range q.entries {
				request := entry.Request
				request.SessionID = session
				if _, err := q.store.Enqueue(ctx, request); err != nil {
					return err
				}
			}
		}
	}
	var err error
	q.entries, err = q.store.FileStore.List(ctx)
	return err
}

func (q *fileQueueFaults) Close() error { return q.store.Close() }
func (q *fileQueueFaults) ReplayState(ctx context.Context) (syncqueue.ReplayState, error) {
	return q.store.ReplayState(ctx)
}
func (q *fileQueueFaults) ReserveLiveSequences(ctx context.Context, count int64) (int64, int64, error) {
	return q.store.ReserveLiveSequences(ctx, count)
}

func newRuntimeWithDeps(t *testing.T, cfg agentconfig.AgentConfig, logger *slog.Logger, client agentruntime.Client, token agentruntime.TokenSource, fingerprint agentruntime.FingerprintSource, interval time.Duration) *agentruntime.Runtime {
	t.Helper()
	return newRuntimeWithRuntimeDeps(t, cfg, logger, client, token, fingerprint, hostsample.New(), probe.New(), interval)
}

func newRuntimeWithRuntimeDeps(t *testing.T, cfg agentconfig.AgentConfig, logger *slog.Logger, client agentruntime.Client, token agentruntime.TokenSource, fingerprint agentruntime.FingerprintSource, host agentruntime.HostSampleProvider, probes agentruntime.ProbeProvider, interval time.Duration, deps ...any) *agentruntime.Runtime {
	t.Helper()
	found := false
	for i, dep := range deps {
		if _, ok := dep.(agentruntime.SyncQueue); ok {
			found = true
		}
		if preparer, ok := dep.(interface{ prepareTestQueue(*testing.T) }); ok {
			preparer.prepareTestQueue(t)
		}
		if queue, ok := dep.(agentruntime.SyncQueue); ok {
			if client, ok := client.(*fakeClient); ok {
				deps[i] = &cancelAfterClientAckQueue{SyncQueue: queue, client: client}
			}
		}
	}
	if !found {
		queue := newRuntimeTestStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{SkipFsync: true})
		if client, ok := client.(*fakeClient); ok {
			deps = append(deps, &cancelAfterClientAckQueue{SyncQueue: queue, client: client})
		} else {
			deps = append(deps, queue)
		}
	}
	return agentruntime.NewWithRuntimeDeps(cfg, logger, client, token, fingerprint, host, probes, interval, deps...)
}

type cancelAfterClientAckQueue struct {
	agentruntime.SyncQueue
	client *fakeClient
}

func (q *cancelAfterClientAckQueue) Delete(ctx context.Context, id string) error {
	if err := q.SyncQueue.Delete(ctx, id); err != nil {
		return err
	}
	if q.client.lastSync.LiveSignal != nil && q.client.cancel != nil && q.client.cancelAfterSyncs > 0 && q.client.syncCalls >= q.client.cancelAfterSyncs {
		q.client.cancel()
		q.client.cancel = nil
	}
	return nil
}
