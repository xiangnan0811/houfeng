//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOrdinaryColdConsecutiveUpgrades(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE")
	committed := os.Getenv("HOUFENG_ORDINARY_COLD_COMMITTED_IMAGE")
	candidate := os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || committed == "" || candidate == "" || released == committed || committed == candidate || released == candidate {
		t.Fatal("expected the two released images and a distinct candidate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixtureMode(t, ctx, released, committed, "host")
	defer f.proxy.Close()
	f.login(t)
	subject := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/vps", map[string]any{"display_name": "consecutive fixture", "lifecycle_status": "active"}, nil))
	content := "# Consecutive cold upgrade fixture\n" + coldRandom(t, 16) + "\n"
	draft := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/record-drafts", map[string]any{
		"payload": f.payload(coldString(t, subject, "vps_id"), content, []string{}),
	}, nil))
	record := f.requestHTTP(t, 201, "POST", "/api/records", map[string]string{
		"draft_id": coldString(t, draft, "draft_id"), "draft_etag": coldString(t, draft, "etag"),
	}, map[string]string{"Idempotency-Key": "consecutive-cold-fixture"})
	recordID := coldString(t, coldJSON(t, record), "record_id")

	// The first hop is performed by the released v1.28.0 controller. Its
	// exact v3 journal is the compatibility input for the second hop.
	f.coldRunUpgrade(t, ctx, "released-commit")
	f.waitHTTP(t)
	f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
	priorRequest, err := ordinaryReadRequestFile(f.request)
	if err != nil {
		t.Fatal(err)
	}
	prior := f.coldIntent(t)
	if prior.Version != 3 || prior.Previous != nil {
		t.Fatal("released controller did not write the v3 journal")
	}
	priorDir := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	guardRoot := filepath.Join(f.work, "maintenance", "ordinary")
	witness := map[string][]byte{}
	for _, path := range []string{filepath.Join(priorDir, "intent.json"), filepath.Join(priorDir, "ready.json"), filepath.Join(priorDir, "restoring.json"), filepath.Join(priorDir, "compose-override.yaml")} {
		witness[path] = coldSuccessorRead(t, path)
	}
	priorState, err := ordinaryObserveDisposition(guardRoot)
	if err != nil || priorState.State != "COMMITTED_READY_FOR_RESTART" || priorState.OperationID != priorRequest.OperationID || priorState.IntentSHA256 != ordinaryDigestHex(witness[filepath.Join(priorDir, "intent.json")]) {
		t.Fatal("first upgrade did not bind the released operation")
	}

	// The predecessor program and database have restarted and the operator
	// has updated the version in .env. Historical recipe/PIDs cannot stand in
	// for current live verification.
	processes, err := ordinaryVerifySelectedProcesses(ctx, prior)
	if err != nil {
		t.Fatal("cannot identify the released target for restart")
	}
	coldCommand(t, ctx, "/usr/bin/docker", "restart", priorRequest.DatabaseContainerID)
	for i := len(processes) - 1; i >= 0; i-- {
		coldCommand(t, ctx, "/usr/bin/docker", "restart", processes[i].ContainerID)
	}
	f.waitHTTP(t)
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := ordinaryVerifySelectedProcesses(ctx, prior); err == nil {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatal("released target did not become healthy after restart")
		}
		time.Sleep(time.Second)
	}
	envPath := filepath.Join(f.work, ".env")
	newEnv := bytes.Replace(coldSuccessorRead(t, envPath), []byte("HOUFENG_IMAGE="+released+"\n"), []byte("HOUFENG_IMAGE="+candidate+"\n"), 1)
	if !bytes.Contains(newEnv, []byte("HOUFENG_IMAGE="+candidate+"\n")) {
		t.Fatal("could not update target image in the fixture environment")
	}
	coldWrite(t, envPath, newEnv, 0o600)

	f.previousOperation, f.operation = f.operation, "next"+coldRandom(t, 8)
	f.targetImage = candidate
	f.admin = filepath.Join(f.work, "successor-admin")
	adminDigest := f.extractImageBinary(t, candidate, "houfeng-record-platform-admin", f.admin)
	if adminDigest == f.targetAdminHash {
		t.Fatal("released and candidate admin executables must differ")
	}
	f.targetAdminHash = adminDigest
	f.targetCenterHash = f.extractImageBinary(t, candidate, "houfeng-center", filepath.Join(f.work, "successor-center"))
	f.targetProcessorHash = f.extractImageBinary(t, candidate, "houfeng-content-processor", filepath.Join(f.work, "successor-processor"))
	next := priorRequest
	next.OperationID, next.TargetImageID = f.operation, candidate
	f.request = filepath.Join(filepath.Dir(f.request), "successor-request.json")
	writeRequest := func(request ordinaryUpgradeRequest) {
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		coldWrite(t, f.request, append(body, '\n'), 0o600)
	}
	refuse := func(phase string, expected ordinaryDisposition) {
		before := f.coldStackSnapshot(t)
		cmd := exec.CommandContext(ctx, f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), phase+":") {
			t.Fatalf("expected %s refusal, got %s (%v)", phase, output, err)
		}
		after, err := ordinaryObserveDisposition(guardRoot)
		if err != nil || after != expected || f.coldStackSnapshot(t) != before {
			t.Fatal("refused upgrade changed the disposition or running stack")
		}
	}
	wrongStorage := next
	wrongStorage.OperationID = "storage" + coldRandom(t, 8)
	wrongStorageEnv := append(bytes.Clone(newEnv), []byte(
		"HOUFENG_ATTACHMENT_BLOB_BACKEND=s3\n"+
			"HOUFENG_ATTACHMENT_S3_ENDPOINT=wrong-attachment-storage.invalid:9000\n"+
			"HOUFENG_ATTACHMENT_S3_BUCKET=other-attachments\n"+
			"HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE=/run/houfeng/optional-secrets/s3/access-key\n"+
			"HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE=/run/houfeng/optional-secrets/s3/secret-key\n")...)
	coldWrite(t, envPath, wrongStorageEnv, 0o600)
	writeRequest(wrongStorage)
	refuse("predecessor-storage", priorState)
	if coldExists(filepath.Join(f.work, "maintenance", "ordinary-operations", wrongStorage.OperationID, "intent.json")) {
		t.Fatal("attachment storage change published an intent")
	}
	coldWrite(t, envPath, newEnv, 0o600)
	writeRequest(next)
	f.assertSuccessorWaitsForSharedGuard(t, guardRoot, priorState)
	f.coldCutBeforeRetry(t, "successor-active", func() {
		interrupted := f.coldIntent(t)
		if interrupted.Version != 4 || interrupted.Previous == nil ||
			*interrupted.Previous != (ordinaryIntentLink{OperationID: priorState.OperationID, IntentSHA256: priorState.IntentSHA256}) ||
			interrupted.Physical.PostmasterStarted.Equal(prior.Physical.PostmasterStarted) {
			t.Fatal("successor did not capture the released intent and current database start")
		}
		wrong := next
		wrong.TargetImageID = released
		writeRequest(wrong)
		refuse("intent", priorState)
		writeRequest(next)
	})
	f.coldRunUpgrade(t, ctx, "successor-exact-retry")
	f.waitHTTP(t)
	f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
	got := coldJSON(t, f.requestHTTP(t, 200, "GET", "/api/records/"+recordID, nil, nil))
	current, ok := got["current"].(map[string]any)
	if !ok || coldString(t, current, "body_markdown") != content {
		t.Fatal("record did not survive both upgrades")
	}
	for path, expected := range witness {
		if !bytes.Equal(coldSuccessorRead(t, path), expected) {
			t.Fatal("successor rewrote the preceding operation")
		}
	}
	finalState, err := ordinaryObserveDisposition(guardRoot)
	if err != nil || finalState.State != "COMMITTED_READY_FOR_RESTART" || finalState.OperationID != next.OperationID {
		t.Fatal("successor did not publish its own ready state")
	}
	writeRequest(priorRequest)
	refuse("intent", finalState)
	legacyTarget := priorRequest
	legacyTarget.OperationID = "legacy" + coldRandom(t, 8)
	writeRequest(legacyTarget)
	refuse("successor-target", finalState)
	if coldExists(filepath.Join(f.work, "maintenance", "ordinary-operations", legacyTarget.OperationID, "intent.json")) {
		t.Fatal("v3-only target published a successor intent")
	}
	writeRequest(next)
	before := f.coldStackSnapshot(t)
	f.coldRunUpgrade(t, ctx, "successor-ready-replay")
	if f.coldStackSnapshot(t) != before {
		t.Fatal("exact replay after the successor started changed the stack")
	}
}

func (f *ordinaryColdFixture) assertSuccessorWaitsForSharedGuard(t *testing.T, guardRoot string, prior ordinaryDisposition) {
	t.Helper()
	lockPath := filepath.Join(guardRoot, ordinaryGuardFile)
	fd, err := unix.Open(lockPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	cmd := exec.CommandContext(ctx, f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	for ctx.Err() == nil {
		if coldSuccessorOpenedExclusiveGuard(cmd.Process.Pid, lockPath) {
			state, err := ordinaryObserveDisposition(guardRoot)
			dir := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
			if err != nil || state != prior || coldExists(dir) {
				t.Fatal("successor changed protected state while a shared reader held the guard")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("successor did not reach the exclusive guard")
}

func coldSuccessorOpenedExclusiveGuard(pid int, lockPath string) bool {
	root := fmt.Sprintf("/proc/%d", pid)
	files, _ := os.ReadDir(filepath.Join(root, "fd"))
	for _, file := range files {
		path, err := os.Readlink(filepath.Join(root, "fd", file.Name()))
		if err != nil || path != lockPath {
			continue
		}
		info, _ := os.ReadFile(filepath.Join(root, "fdinfo", file.Name()))
		for _, line := range strings.Split(string(info), "\n") {
			if text, ok := strings.CutPrefix(line, "flags:"); ok {
				mode, err := strconv.ParseUint(strings.TrimSpace(text), 8, 64)
				if err == nil && mode&unix.O_ACCMODE == unix.O_RDWR {
					return true
				}
			}
		}
	}
	return false
}

func coldSuccessorRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
