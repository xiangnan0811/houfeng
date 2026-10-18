// Package syncqueue persists the agent's replay authority and bounded FIFO.
// A queue must be opened for an explicit session before use. Its separate lock
// inode is held until Close, including while queue snapshots are atomically replaced.
package syncqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"houfeng/internal/contracts/agentapi"
)

const (
	defaultMaxEntries = 65536
	defaultMaxAge     = 72 * time.Hour
	defaultMaxBytes   = 64 * 1024 * 1024
)

var (
	errEntryExceedsCapacity = errors.New("sync queue entry exceeds configured capacity")
	// ErrLegacyQueueNotDrained requires an explicit old-agent drain before
	// protocol migration. Callers may expose static recovery guidance for it.
	ErrLegacyQueueNotDrained = errors.New("nonempty legacy sync queue: restore the previous agent and drain it before upgrading")
	// ErrAmbiguousDurability means rename committed but directory fsync failed.
	// The visible snapshot must not be rolled back or reused through this store.
	// Close it and recover with a new FileStore and OpenSession.
	ErrAmbiguousDurability = errors.New("sync queue durability is ambiguous; reopen with a fresh store")
)

type Options struct {
	MaxEntries int
	MaxAge     time.Duration
	MaxBytes   int
	// SkipFsync is for tests only. Production must retain crash durability.
	SkipFsync bool
}

type Entry struct {
	ID        string               `json:"id"`
	CreatedAt time.Time            `json:"created_at"`
	Attempts  int                  `json:"attempts"`
	Request   agentapi.SyncRequest `json:"request"`
}

func (entry *Entry) UnmarshalJSON(payload []byte) error {
	var stored struct {
		ID        *string               `json:"id"`
		CreatedAt *time.Time            `json:"created_at"`
		Attempts  *int                  `json:"attempts"`
		Request   *agentapi.SyncRequest `json:"request"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return err
	}
	if stored.ID == nil || stored.CreatedAt == nil || stored.Attempts == nil || stored.Request == nil {
		return errors.New("incomplete sync queue entry")
	}
	*entry = Entry{ID: *stored.ID, CreatedAt: *stored.CreatedAt, Attempts: *stored.Attempts, Request: *stored.Request}
	return nil
}

type ReplayState struct {
	SessionID        string
	ProducerID       string
	AllocatedThrough int64
	RetiredThrough   int64
}

type envelope struct {
	Version               int     `json:"version"`
	SessionID             string  `json:"session_id"`
	ProducerID            string  `json:"producer_id"`
	LastAllocatedBatchSeq int64   `json:"last_allocated_batch_seq"`
	RetiredBatchFloor     int64   `json:"retired_batch_floor"`
	LiveReservedThrough   int64   `json:"live_reserved_through"`
	Entries               []Entry `json:"entries"`
}

type FileStore struct {
	path    string
	opts    Options
	now     func() time.Time
	mu      sync.Mutex
	lock    *os.File
	session string
	poison  error
	// Per-store hooks keep fault injection independent of other queues.
	rename   func(string, string) error
	syncFile func(*os.File) error
	syncDir  func(string) error
}

func NewFileStore(path string, opts Options) *FileStore {
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = defaultMaxEntries
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = defaultMaxAge
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	return &FileStore{path: path, opts: opts, now: func() time.Time { return time.Now().UTC() }, rename: os.Rename, syncFile: (*os.File).Sync, syncDir: syncDirectory}
}

func (s *FileStore) SetNowForTest(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	s.now = now
}

// OpenSession validates existing bytes before writing anything. Only a missing
// file or an empty legacy array can bootstrap. An explicit session change first
// durably retires the old authority's entries, then initializes a new producer.
func (s *FileStore) OpenSession(ctx context.Context, session string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison != nil {
		return s.poison
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if session == "" {
		return errors.New("sync queue session is required")
	}
	if s.lock != nil {
		if session == s.session {
			_, err := s.readEnvelope()
			return err
		}
		return errors.New("close sync queue before changing session")
	}
	dir := filepath.Dir(s.path)
	if err := ensurePrivateDir(dir, s.syncDir); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return fmt.Errorf("lock sync queue: %w", err)
	}
	s.lock = lock
	opened := false
	defer func() {
		if !opened {
			_ = s.releaseLock()
		}
	}()
	payload, err := os.ReadFile(s.path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	bootstrap := missing
	var state envelope
	if !missing {
		trimmed := bytes.TrimSpace(payload)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			var legacy []json.RawMessage
			if err := json.Unmarshal(trimmed, &legacy); err != nil {
				return fmt.Errorf("invalid legacy sync queue: %w", err)
			}
			if len(legacy) != 0 {
				return ErrLegacyQueueNotDrained
			}
			bootstrap = true
		} else {
			state, err = decodeEnvelope(payload)
			if err != nil {
				return err
			}
		}
	}
	if !bootstrap && state.SessionID != session {
		state.Entries = []Entry{}
		state.RetiredBatchFloor = state.LastAllocatedBatchSeq
		if err := s.writeEnvelope(ctx, state); err != nil {
			return err
		}
		bootstrap = true
	}
	if bootstrap {
		producer, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		state = envelope{Version: 2, SessionID: session, ProducerID: producer.String(), Entries: []Entry{}}
		if err := s.writeEnvelope(ctx, state); err != nil {
			return err
		}
	}
	s.session = session
	opened = true
	return nil
}

func (s *FileStore) releaseLock() error {
	if s.lock == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	closeErr := s.lock.Close()
	s.lock = nil
	s.session = ""
	return errors.Join(unlockErr, closeErr)
}

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.poison, s.releaseLock())
}

func (s *FileStore) ready(ctx context.Context) error {
	if s.poison != nil {
		return s.poison
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.lock == nil {
		return errors.New("sync queue requires OpenSession")
	}
	return nil
}

func (s *FileStore) ReplayState(ctx context.Context) (ReplayState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return ReplayState{}, err
	}
	state, err := s.readEnvelope()
	if err != nil {
		return ReplayState{}, err
	}
	if err := ctx.Err(); err != nil {
		return ReplayState{}, err
	}
	return ReplayState{SessionID: state.SessionID, ProducerID: state.ProducerID, AllocatedThrough: state.LastAllocatedBatchSeq, RetiredThrough: state.RetiredBatchFloor}, nil
}

func (s *FileStore) ReserveLiveSequences(ctx context.Context, count int64) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, 0, err
	}
	if count != agentapi.LiveSequenceReservation {
		return 0, 0, errors.New("live sequence reservation must contain 1024 sequences")
	}
	state, err := s.readEnvelope()
	if err != nil {
		return 0, 0, err
	}
	if state.LiveReservedThrough > math.MaxInt64-count {
		return 0, 0, errors.New("live sequence overflow")
	}
	first := state.LiveReservedThrough + 1
	state.LiveReservedThrough += count
	if err := s.writeEnvelope(ctx, state); err != nil {
		return 0, 0, err
	}
	return first, state.LiveReservedThrough, nil
}

func (s *FileStore) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return "", err
	}
	state, err := s.readEnvelope()
	if err != nil {
		return "", err
	}
	if request.SessionID != state.SessionID {
		return "", errors.New("sync queue request session mismatch")
	}
	if state.LastAllocatedBatchSeq == math.MaxInt64 {
		return "", errors.New("batch sequence overflow")
	}
	request = cloneRequest(request)
	request.LiveSignal = nil
	state.LastAllocatedBatchSeq++
	request.ProtocolRevision = agentapi.SyncProtocolRevision
	request.ProducerID = state.ProducerID
	request.BatchSequence = state.LastAllocatedBatchSeq
	request.AllocatedThrough = state.LastAllocatedBatchSeq
	request.RetiredThrough = state.RetiredBatchFloor
	id := state.ProducerID + ":" + strconv.FormatInt(request.BatchSequence, 10)
	setBatchIdentity(&request, id)
	createdAt := s.now()
	if len(state.Entries) > 0 && !createdAt.After(state.Entries[len(state.Entries)-1].CreatedAt) {
		createdAt = state.Entries[len(state.Entries)-1].CreatedAt.Add(time.Nanosecond)
	}
	state.Entries = append(state.Entries, Entry{ID: id, CreatedAt: createdAt, Request: request})
	if err := s.pruneEnvelope(&state); err != nil {
		return "", err
	}
	if len(state.Entries) == 0 || state.Entries[len(state.Entries)-1].ID != id {
		return "", errEntryExceedsCapacity
	}
	if err := s.writeEnvelope(ctx, state); err != nil {
		return "", err
	}
	return id, nil
}

func setBatchIdentity(request *agentapi.SyncRequest, id string) {
	for i := range request.Heartbeats {
		request.Heartbeats[i].SyncBatchID = id
	}
	for i := range request.HostSamples {
		request.HostSamples[i].SyncBatchID = id
	}
	for i := range request.ProbeObservations {
		request.ProbeObservations[i].SyncBatchID = id
	}
	for i := range request.IPQualityReports {
		request.IPQualityReports[i].SyncBatchID = id
	}
}

func (s *FileStore) List(ctx context.Context) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	state, err := s.readEnvelope()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return state.Entries, nil
}

func (s *FileStore) Delete(ctx context.Context, id string) error {
	return s.DeleteMany(ctx, []string{id})
}

func (s *FileStore) DeleteMany(ctx context.Context, ids []string) error {
	return s.mutate(ctx, func(state *envelope) error {
		deleted := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			deleted[id] = struct{}{}
		}
		kept := state.Entries[:0]
		for _, entry := range state.Entries {
			if _, remove := deleted[entry.ID]; !remove {
				kept = append(kept, entry)
			}
		}
		state.Entries = kept
		advanceFloor(state)
		return nil
	})
}

func (s *FileStore) MarkAttempt(ctx context.Context, id string) error {
	return s.mutate(ctx, func(state *envelope) error {
		for i := range state.Entries {
			if state.Entries[i].ID == id {
				if state.Entries[i].Attempts == math.MaxInt {
					return errors.New("sync queue attempt count overflow")
				}
				state.Entries[i].Attempts++
				break
			}
		}
		return nil
	})
}

func (s *FileStore) Prune(ctx context.Context) error {
	return s.mutate(ctx, s.pruneEnvelope)
}

func (s *FileStore) mutate(ctx context.Context, mutation func(*envelope) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	state, err := s.readEnvelope()
	if err != nil {
		return err
	}
	if err := mutation(&state); err != nil {
		return err
	}
	return s.writeEnvelope(ctx, state)
}

func advanceFloor(state *envelope) {
	floor := state.LastAllocatedBatchSeq
	if len(state.Entries) > 0 {
		floor = state.Entries[0].Request.BatchSequence - 1
	}
	if floor > state.RetiredBatchFloor {
		state.RetiredBatchFloor = floor
	}
}

// Measure entry payloads once; account for envelope fields (including the
// changing floor's decimal width) as oldest entries are removed in a linear pass.
func (s *FileStore) pruneEnvelope(state *envelope) error {
	cutoff := s.now().Add(-s.opts.MaxAge)
	first := 0
	for first < len(state.Entries) && state.Entries[first].CreatedAt.Before(cutoff) {
		first++
	}
	if len(state.Entries)-first > s.opts.MaxEntries {
		first = len(state.Entries) - s.opts.MaxEntries
	}
	state.Entries = state.Entries[first:]
	sizes := make([]int, len(state.Entries))
	total := 0
	for i, entry := range state.Entries {
		payload, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		sizes[i] = len(payload)
		total += len(payload)
	}
	for {
		advanceFloor(state)
		metadata := *state
		metadata.Entries = []Entry{}
		payload, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		commas := max(0, len(state.Entries)-1)
		if len(payload)+total+commas <= s.opts.MaxBytes {
			return nil
		}
		if len(state.Entries) == 0 {
			return errEntryExceedsCapacity
		}
		total -= sizes[0]
		sizes = sizes[1:]
		state.Entries = state.Entries[1:]
	}
}

func (s *FileStore) readEnvelope() (envelope, error) {
	payload, err := os.ReadFile(s.path)
	if err != nil {
		return envelope{}, err
	}
	state, err := decodeEnvelope(payload)
	if err == nil && s.session != "" && state.SessionID != s.session {
		return envelope{}, errors.New("sync queue session changed while open")
	}
	return state, err
}

func decodeEnvelope(payload []byte) (envelope, error) {
	var state envelope
	if err := rejectDuplicateKeys(json.NewDecoder(bytes.NewReader(payload))); err != nil {
		return state, fmt.Errorf("invalid sync queue JSON: %w", err)
	}
	// Reject absent/null control fields instead of silently treating them as zero.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return state, fmt.Errorf("invalid sync queue envelope: %w", err)
	}
	for _, field := range []string{"version", "session_id", "producer_id", "last_allocated_batch_seq", "retired_batch_floor", "live_reserved_through", "entries"} {
		value, exists := fields[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return state, fmt.Errorf("sync queue envelope requires %s", field)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, fmt.Errorf("invalid sync queue envelope: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return state, errors.New("trailing sync queue data")
	}
	if state.Version != 2 || state.SessionID == "" || !agentapi.ValidSyncProducerID(state.ProducerID) ||
		state.LastAllocatedBatchSeq < 0 || state.RetiredBatchFloor < 0 || state.RetiredBatchFloor > state.LastAllocatedBatchSeq || state.LiveReservedThrough < 0 || state.Entries == nil {
		return state, errors.New("invalid sync queue replay bounds")
	}
	previous := state.RetiredBatchFloor
	var previousTime time.Time
	for _, entry := range state.Entries {
		r := entry.Request
		identity := state.ProducerID + ":" + strconv.FormatInt(r.BatchSequence, 10)
		if r.ProtocolRevision != agentapi.SyncProtocolRevision || r.SessionID != state.SessionID || r.ProducerID != state.ProducerID ||
			r.BatchSequence <= previous || r.BatchSequence > state.LastAllocatedBatchSeq || r.AllocatedThrough < r.BatchSequence || r.AllocatedThrough > state.LastAllocatedBatchSeq ||
			r.RetiredThrough < 0 || r.RetiredThrough >= r.BatchSequence || r.RetiredThrough > state.RetiredBatchFloor || r.LiveSignal != nil ||
			entry.ID != identity || entry.CreatedAt.IsZero() || (!previousTime.IsZero() && !entry.CreatedAt.After(previousTime)) || entry.Attempts < 0 {
			return state, errors.New("invalid sync queue entry identity or bounds")
		}
		if !batchIdentityMatches(r, identity) {
			return state, errors.New("invalid sync queue fact identity")
		}
		previous = r.BatchSequence
		previousTime = entry.CreatedAt
	}
	expectedFloor := state.LastAllocatedBatchSeq
	if len(state.Entries) > 0 {
		expectedFloor = state.Entries[0].Request.BatchSequence - 1
	}
	if state.RetiredBatchFloor != expectedFloor {
		return state, errors.New("invalid sync queue retired floor")
	}
	return state, nil
}

// JSON duplicate keys must not silently replace replay authority or payload.
func rejectDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", name)
			}
			seen[name] = struct{}{}
			if err := rejectDuplicateKeys(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := rejectDuplicateKeys(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func batchIdentityMatches(r agentapi.SyncRequest, id string) bool {
	for _, fact := range r.Heartbeats {
		if fact.SyncBatchID != id {
			return false
		}
	}
	for _, fact := range r.HostSamples {
		if fact.SyncBatchID != id {
			return false
		}
	}
	for _, fact := range r.ProbeObservations {
		if fact.SyncBatchID != id {
			return false
		}
	}
	for _, fact := range r.IPQualityReports {
		if fact.SyncBatchID != id {
			return false
		}
	}
	return true
}

func (s *FileStore) writeEnvelope(ctx context.Context, state envelope) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(payload) > s.opts.MaxBytes {
		return errEntryExceedsCapacity
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	file, err := os.CreateTemp(dir, ".sync-buffer-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if !s.opts.SkipFsync {
		if err := s.syncFile(file); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.rename(tmpPath, s.path); err != nil {
		return err
	}
	if !s.opts.SkipFsync {
		if err := s.syncDir(dir); err != nil {
			s.poison = fmt.Errorf("%w: %v", ErrAmbiguousDurability, err)
			return s.poison
		}
	}
	return nil
}

func ensurePrivateDir(dir string, syncDir func(string) error) error {
	created, err := missingDirs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, createdDir := range created {
		if err := os.Chmod(createdDir, 0o700); err != nil {
			return err
		}
	}
	if dir != "." {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	for _, createdDir := range created {
		if err := syncDir(filepath.Dir(createdDir)); err != nil {
			return err
		}
	}
	return nil
}

func missingDirs(dir string) ([]string, error) {
	dir = filepath.Clean(dir)
	var missing []string
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		} else if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, dir)
		} else {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for left, right := 0, len(missing)-1; left < right; left, right = left+1, right-1 {
		missing[left], missing[right] = missing[right], missing[left]
	}
	return missing, nil
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func WithBackfilledFacts(request agentapi.SyncRequest, backfilled bool) agentapi.SyncRequest {
	cloned := cloneRequest(request)
	cloned.LiveSignal = nil
	for i := range cloned.Heartbeats {
		cloned.Heartbeats[i].IsBackfilled = backfilled
	}
	for i := range cloned.HostSamples {
		cloned.HostSamples[i].IsBackfilled = backfilled
	}
	for i := range cloned.ProbeObservations {
		cloned.ProbeObservations[i].IsBackfilled = backfilled
	}
	for i := range cloned.IPQualityReports {
		cloned.IPQualityReports[i].IsBackfilled = backfilled
	}
	return cloned
}

func cloneRequest(request agentapi.SyncRequest) agentapi.SyncRequest {
	cloned := request
	cloned.Heartbeats = append([]agentapi.MonitoringInstanceHeartbeat(nil), request.Heartbeats...)
	cloned.HostSamples = append([]agentapi.HostSamplePayload(nil), request.HostSamples...)
	cloned.ProbeObservations = append([]agentapi.ProbeObservationPayload(nil), request.ProbeObservations...)
	cloned.IPQualityReports = cloneIPQualityReports(request.IPQualityReports)
	return cloned
}

func cloneIPQualityReports(reports []agentapi.IPQualityReportPayload) []agentapi.IPQualityReportPayload {
	if len(reports) == 0 {
		return nil
	}
	cloned := make([]agentapi.IPQualityReportPayload, len(reports))
	for i, report := range reports {
		cloned[i] = report
		cloned[i].RawJSON = append([]byte(nil), report.RawJSON...)
		cloned[i].ProviderResults = append([]agentapi.IPQualityProviderResultPayload(nil), report.ProviderResults...)
		cloned[i].ServiceUnlocks = append([]agentapi.IPQualityServiceUnlockPayload(nil), report.ServiceUnlocks...)
		cloned[i].DiagnosticsJSON = append([]byte(nil), report.DiagnosticsJSON...)
		cloned[i].Coverage = clonePointer(report.Coverage)
		cloneIPQualityProductFields(&cloned[i])
	}
	return cloned
}
