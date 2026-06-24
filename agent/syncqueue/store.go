package syncqueue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
)

const (
	defaultMaxEntries = 65536
	defaultMaxAge     = 72 * time.Hour
	defaultMaxBytes   = 64 * 1024 * 1024
)

type Options struct {
	MaxEntries int
	MaxAge     time.Duration
	MaxBytes   int
	// SkipFsync, when true, skips the file.Sync() and syncDir() calls in
	// writeEntries. fsync provides crash durability — losing a queue file's
	// last write on power loss. Tests that exercise runtime retry/restart
	// logic on tight timing budgets do not need disk durability and would
	// otherwise be dominated by macOS APFS fsync latency (10-30ms each).
	// Production callers MUST leave this false.
	SkipFsync bool
}

type Entry struct {
	ID        string               `json:"id"`
	CreatedAt time.Time            `json:"created_at"`
	Attempts  int                  `json:"attempts"`
	Request   agentapi.SyncRequest `json:"request"`
}

type storedEntry struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Attempts  int               `json:"attempts"`
	Request   storedSyncRequest `json:"request"`
}

type storedSyncRequest struct {
	agentapi.SyncRequest
	LegacyNodeID string `json:"node_id"`
}

func (r *storedSyncRequest) UnmarshalJSON(payload []byte) error {
	type syncRequestAlias agentapi.SyncRequest
	var stored struct {
		syncRequestAlias
		LegacyNodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(payload, &stored); err != nil {
		return err
	}
	r.SyncRequest = agentapi.SyncRequest(stored.syncRequestAlias)
	if r.MonitoringInstanceID == "" {
		r.MonitoringInstanceID = stored.LegacyNodeID
	}
	r.LegacyNodeID = stored.LegacyNodeID
	return nil
}

type FileStore struct {
	path string
	opts Options
	now  func() time.Time
	mu   *sync.Mutex
}

var fileStoreLocks sync.Map

func lockForPath(path string) *sync.Mutex {
	key := filepath.Clean(path)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	lock, _ := fileStoreLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
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
	return &FileStore{
		path: path,
		opts: opts,
		now:  func() time.Time { return time.Now().UTC() },
		mu:   lockForPath(path),
	}
}

func (s *FileStore) SetNowForTest(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
		return
	}
	s.now = now
}

func (s *FileStore) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntries()
	if err != nil {
		return "", err
	}
	now := s.now()
	createdAt := now
	if len(entries) > 0 {
		last := entries[len(entries)-1].CreatedAt
		if !createdAt.After(last) {
			createdAt = last.Add(time.Nanosecond)
		}
	}
	id := entryIDForRequest(request, now, entries)
	entries = append(entries, Entry{
		ID:        id,
		CreatedAt: createdAt,
		Request:   cloneRequest(request),
	})
	entries = s.pruneEntries(entries)
	if err := s.writeEntries(entries); err != nil {
		return "", err
	}
	return id, nil
}

func (s *FileStore) List(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}
	sortEntries(entries)
	out := make([]Entry, len(entries))
	for i, entry := range entries {
		out[i] = Entry{
			ID:        entry.ID,
			CreatedAt: entry.CreatedAt,
			Attempts:  entry.Attempts,
			Request:   cloneRequest(entry.Request),
		}
	}
	return out, nil
}

func (s *FileStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntries()
	if err != nil {
		return err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.ID != id {
			filtered = append(filtered, entry)
		}
	}
	return s.writeEntries(filtered)
}

func (s *FileStore) MarkAttempt(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntries()
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID == id {
			entries[i].Attempts++
			break
		}
	}
	return s.writeEntries(entries)
}

func (s *FileStore) Prune(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readEntries()
	if err != nil {
		return err
	}
	return s.writeEntries(s.pruneEntries(entries))
}

func WithBackfilledFacts(request agentapi.SyncRequest, backfilled bool) agentapi.SyncRequest {
	cloned := cloneRequest(request)
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

func (s *FileStore) pruneEntries(entries []Entry) []Entry {
	if len(entries) == 0 {
		return entries
	}
	sortEntries(entries)
	cutoff := s.now().Add(-s.opts.MaxAge)
	pruned := entries[:0]
	for _, entry := range entries {
		if entry.CreatedAt.Before(cutoff) {
			continue
		}
		pruned = append(pruned, entry)
	}
	if len(pruned) > s.opts.MaxEntries {
		pruned = pruned[len(pruned)-s.opts.MaxEntries:]
	}
	pruned = pruneEntriesByBytes(pruned, s.opts.MaxBytes)
	result := make([]Entry, len(pruned))
	copy(result, pruned)
	return result
}

func pruneEntriesByBytes(entries []Entry, maxBytes int) []Entry {
	for len(entries) > 0 {
		payload, err := json.Marshal(entries)
		if err != nil || len(payload) <= maxBytes {
			return entries
		}
		entries = entries[1:]
	}
	return entries
}

func (s *FileStore) readEntries() ([]Entry, error) {
	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, err
	}
	if len(payload) == 0 {
		return []Entry{}, nil
	}
	var storedEntries []storedEntry
	if err := json.Unmarshal(payload, &storedEntries); err != nil {
		return nil, err
	}
	entries := make([]Entry, len(storedEntries))
	for i, entry := range storedEntries {
		entries[i] = Entry{
			ID:        entry.ID,
			CreatedAt: entry.CreatedAt,
			Attempts:  entry.Attempts,
			Request:   agentapi.SyncRequest(entry.Request.SyncRequest),
		}
	}
	sortEntries(entries)
	return entries, nil
}

func (s *FileStore) writeEntries(entries []Entry) error {
	dir := filepath.Dir(s.path)
	if dir != "." {
		if err := ensurePrivateDir(dir); err != nil {
			return err
		}
	}
	sortEntries(entries)
	payload, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".sync-buffer-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if !s.opts.SkipFsync {
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	cleanup = false
	if s.opts.SkipFsync {
		return nil
	}
	return syncDir(dir)
}

func entryIDForRequest(request agentapi.SyncRequest, now time.Time, entries []Entry) string {
	if len(request.Heartbeats) > 0 && request.Heartbeats[0].SyncBatchID != "" {
		return request.Heartbeats[0].SyncBatchID
	}
	base := now.UTC().Format(time.RFC3339Nano)
	if !entryIDExists(entries, base) {
		return base
	}
	for suffix := 1; ; suffix++ {
		candidate := base + "-" + strconv.Itoa(suffix)
		if !entryIDExists(entries, candidate) {
			return candidate
		}
	}
}

func entryIDExists(entries []Entry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func ensurePrivateDir(dir string) error {
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
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
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

var syncDir = syncDirectory

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
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
	}
	return cloned
}
