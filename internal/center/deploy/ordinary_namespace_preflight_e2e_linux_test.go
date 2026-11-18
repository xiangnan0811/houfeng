//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A populated released tree must be refused while the guard stays CLEAN.
func TestOrdinaryColdNamespacePreflightRefusesUnboundTree(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released, target := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || target == "" || released == target {
		t.Fatal("missing isolated released and target images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, released, target)
	defer f.proxy.Close()
	root := filepath.Join(f.work, "data", "attachments")
	if _, err := os.Stat(filepath.Join(root, "sha256")); err != nil {
		t.Fatal("released predecessor did not create its legacy BlobRoot")
	}
	if err := os.WriteFile(filepath.Join(root, "sha256", "legacy-blob"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "namespace-preflight") {
		t.Fatal("unbound physical namespace was not refused at preflight")
	}
	if coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json")) != "CLEAN" {
		t.Fatal("preflight refusal activated the ordinary guard")
	}
	operation := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	if coldExists(filepath.Join(operation, "intent.json")) || coldExists(filepath.Join(f.work, ".ordinary-fences", f.operation, "fences", f.operation+".json")) {
		t.Fatal("preflight refusal published intent or fenced containers")
	}
	f.requestHTTP(t, 200, "GET", "/api/healthz", nil, nil)
}

func TestOrdinaryColdNamespaceMissingAndEmptyRefused(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released, target := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || target == "" || released == target {
		t.Fatal("missing isolated released and target images")
	}
	for _, state := range []string{"missing", "empty"} {
		if !t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			f := newOrdinaryColdFixture(t, ctx, released, target)
			defer f.proxy.Close()
			root := filepath.Join(f.work, "data", "attachments")
			if err := os.Remove(filepath.Join(root, "sha256")); err != nil {
				t.Fatal("released skeleton is not empty:", err)
			}
			// A missing root is an unresolvable live bind source, so the fence
			// inventory refuses it before the namespace preflight can run.
			phase := "namespace-preflight"
			if state == "missing" {
				if err := os.Remove(root); err != nil {
					t.Fatal(err)
				}
				phase = "fence-inventory"
			}
			before := f.coldStackSnapshot(t)
			f.coldAssertPreflightRefusal(t, phase, before)
			f.requestHTTP(t, 200, "GET", "/api/healthz", nil, nil)
		}) {
			t.FailNow()
		}
	}
}

// The event is held before its process is identified. A container ID, process
// start time, executable digest and cgroup must all agree before any signal.
func TestOrdinaryColdNamespaceMarkerProcessKill(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released, target := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || target == "" || released == target {
		t.Fatal("missing isolated released and target images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, released, target)
	defer f.proxy.Close()
	root := filepath.Join(f.work, "data", "attachments")
	marker := filepath.Join(root, ".houfeng-local-namespace")
	if coldExists(marker) {
		t.Fatal("released skeleton already contains marker")
	}
	group := coldFanotify(t, root)
	cmd := exec.CommandContext(ctx, f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	log, err := os.OpenFile("/root/fixture-evidence/namespace-marker-cli.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = unix.Close(group)
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = unix.Close(group)
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	w := &coldPermissionWatcher{stop: make(chan struct{}), done: make(chan struct{}), faults: make(chan error, 1), events: make(chan coldPermissionEvent, 64), decisions: make(chan coldPermissionDecision, 64), namespaceRoot: root}
	go w.run(group, cmd.Process.Pid)
	actorFD := -1
	actorValidated := false
	defer func() {
		_ = cmd.Process.Kill()
		w.stopWatching()
		<-w.done
		if actorFD >= 0 {
			if actorValidated && !w.signalDelivered.Load() {
				_ = unix.PidfdSendSignal(actorFD, unix.SIGKILL, nil, 0)
			}
			_ = unix.Close(actorFD)
		}
	}()
	var event coldPermissionEvent
	select {
	case event = <-w.events:
	case err := <-finished:
		t.Fatalf("controller exited before namespace marker open: %v", err)
	case err := <-w.faults:
		t.Fatalf("marker watcher failed: %v", err)
	case <-ctx.Done():
		t.Fatal("marker open was not observed before deadline")
	}
	visiblePath, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", event.fd))
	if err != nil {
		t.Fatal("cannot resolve held marker event")
	}
	path := filepath.Join(root, filepath.Base(visiblePath))
	identityBody, err := os.ReadFile(filepath.Join(f.work, "data", "local-namespace-init", "identity.json"))
	var identity composeNamespaceIdentity
	if err != nil || json.Unmarshal(identityBody, &identity) != nil || !validComposeNamespaceUUID(identity.RequestID) || filepath.Base(path) != ".houfeng-local-namespace.tmp-"+identity.RequestID {
		t.Fatal("held marker does not match durable namespace request identity")
	}
	var held, source unix.Stat_t
	if unix.Fstat(event.fd, &held) != nil || unix.Lstat(path, &source) != nil || source.Dev != held.Dev || source.Ino != held.Ino || held.Mode&unix.S_IFMT != unix.S_IFREG || held.Mode&07777 != 0600 || held.Uid != 10001 || held.Gid != 10001 || held.Nlink != 1 || held.Size != 0 || coldExists(marker) {
		t.Fatal("marker open is not the empty pre-publication state in the BlobRoot bind source")
	}
	op := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	if coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json")) != "COMMITTED_READY_FOR_RESTART" || !coldExists(filepath.Join(op, "ready.json")) {
		t.Fatal("namespace marker open was not during committed cold recreation")
	}
	start, err := ordinaryNativeStart(event.pid)
	if err != nil {
		t.Fatal("cannot read namespace process start time")
	}
	actorFD, err = unix.PidfdOpen(event.pid, 0)
	if err != nil {
		t.Fatal("cannot pin namespace process")
	}
	cgroup, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", event.pid))
	if err != nil {
		t.Fatal("cannot identify namespace process cgroup")
	}
	exe, err := os.Open(fmt.Sprintf("/proc/%d/exe", event.pid))
	if err != nil {
		t.Fatal("cannot inspect namespace executable")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, exe)
	closeErr := exe.Close()
	if copyErr != nil || closeErr != nil || fmt.Sprintf("%x", hash.Sum(nil)) != fmt.Sprintf("%x", f.targetAdminHash) {
		t.Fatal("namespace process is not the selected target admin executable")
	}
	ids := strings.Fields(string(coldCommand(t, ctx, "/usr/bin/docker", "ps", "--all", "--no-trunc", "--quiet", "--filter", "label=com.docker.compose.project="+f.project, "--filter", "label=com.docker.compose.service=houfeng-local-namespace-init")))
	matched := ""
	for _, id := range ids {
		if !fenceID.MatchString(id) {
			t.Fatal("invalid namespace container ID")
		}
		observation := strings.TrimSpace(string(coldCommand(t, ctx, "/usr/bin/docker", "inspect", "--format", `{{.Id}}|{{.State.Pid}}|{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{.Image}}`, id)))
		if observation == fmt.Sprintf("%s|%d|%s|houfeng-local-namespace-init|%s", id, event.pid, f.project, f.targetImage) && strings.Contains(string(cgroup), id) {
			if matched != "" {
				t.Fatal("namespace process matched more than one container")
			}
			matched = id
		}
	}
	afterStart, err := ordinaryNativeStart(event.pid)
	if matched == "" || err != nil || start != afterStart {
		t.Fatal("held namespace process could not be tied to the exact Compose container")
	}
	poll := []unix.PollFd{{Fd: int32(actorFD), Events: unix.POLLIN}}
	if _, err := unix.Poll(poll, 0); err != nil || poll[0].Revents != 0 {
		t.Fatal("namespace process exited before the held cut")
	}
	witness := func(phase string) {
		body, err := json.Marshal(struct {
			Phase, Operation, Container, Path string
			PID                               int
			Start, Device, Inode              uint64
			MarkerPresent                     bool
		}{phase, f.operation, matched, path, event.pid, start, uint64(held.Dev), held.Ino, coldExists(marker)})
		if err != nil {
			t.Fatal(err)
		}
		coldPersistEvidence(t, f.operation+"-namespace-marker-"+phase+".json", append(body, '\n'))
	}
	witness("pre-kill")
	actorValidated = true
	if err := w.killAndDeny(ctx, event, 0, actorFD); err != nil {
		t.Fatalf("namespace pidfd kill and denial failed: %v", err)
	}
	w.stopWatching()
	<-w.done
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("controller succeeded despite killed initializer")
		}
	case <-ctx.Done():
		t.Fatal("controller did not stop after initializer was killed")
	}
	var named unix.Stat_t
	if coldExists(marker) || unix.Lstat(path, &named) != nil || named.Dev != held.Dev || named.Ino != held.Ino || named.Size != 0 || named.Nlink != 1 {
		t.Fatal("killed initializer did not leave the witnessed unpublished marker temp")
	}
	if coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json")) != "COMMITTED_READY_FOR_RESTART" || !coldExists(filepath.Join(op, "ready.json")) {
		t.Fatal("interrupted recreation changed the committed operation state")
	}
	witness("pre-retry")
	f.coldRunUpgrade(t, ctx, "namespace-marker-retry")
	f.waitHTTP(t)
	if !coldExists(marker) {
		t.Fatal("exact retry did not publish the namespace marker")
	}
	f.requestHTTP(t, 200, "GET", "/api/healthz", nil, nil)
}

func TestOrdinaryColdNamespaceLocalRowsRefused(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released, target := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || target == "" || released == target {
		t.Fatal("missing isolated released and target images")
	}
	for _, afterIntent := range []bool{false, true} {
		t.Run(map[bool]string{false: "preflight", true: "recheck"}[afterIntent], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			f := newOrdinaryColdFixture(t, ctx, released, target)
			defer f.proxy.Close()
			request, err := ordinaryReadRequestFile(f.request)
			if err != nil {
				t.Fatal(err)
			}
			psql := func(sql string) string {
				return strings.TrimSpace(string(coldCommand(t, ctx, "/usr/bin/docker", "exec", "--user", "postgres", request.DatabaseContainerID, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "houfeng", "-c", sql)))
			}
			insert := func() {
				psql(`insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind) values ('sha256/'||repeat('1',64),decode(repeat('1',64),'hex'),'fixture',1,'local')`)
			}
			if afterIntent {
				f.coldCutBeforeRetry(t, "fence", insert)
			} else {
				insert()
			}
			before := psql(`select count(*) from public.deployment_membership where deployment_epoch=2`)
			command := exec.CommandContext(ctx, f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
			command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
			output, runErr := command.CombinedOutput()
			phase, disposition := "namespace-preflight", "CLEAN"
			if afterIntent {
				phase, disposition = "namespace-recheck", "ACTIVE"
			}
			if runErr == nil || !strings.Contains(string(output), phase) {
				t.Fatalf("expected %s refusal: %s", phase, output)
			}
			if coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json")) != disposition {
				t.Fatal("unexpected disposition")
			}
			operation := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
			participants, err := filepath.Glob(filepath.Join(operation, "participant-*.json"))
			if err != nil || len(participants) != 0 {
				t.Fatal("participant was spawned")
			}
			if got := psql(`select count(*) from public.deployment_membership where deployment_epoch=2`); got != before {
				t.Fatal("projection changed on refusal")
			}
		})
	}
}

// Reconstruct the durable identity-only state left by an interrupted namespace
// initializer and prove the same cold operation resumes through recreation.
func TestOrdinaryColdNamespaceInterruptedInitializerReplay(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released, target := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || target == "" || released == target {
		t.Fatal("missing isolated released and target images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, released, target)
	defer f.proxy.Close()
	identity := composeNamespaceIdentity{Version: 1, NamespaceID: "11111111-1111-4111-8111-111111111111", IncarnationID: "22222222-2222-4222-8222-222222222222", RequestID: "33333333-3333-4333-8333-333333333333"}
	body, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	f.coldCutBeforeRetry(t, "ready", func() {
		dir := filepath.Join(f.work, "data", "local-namespace-init")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(dir, 10001, 10001); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "identity.json")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 10001, 10001); err != nil {
			t.Fatal(err)
		}
	})
	f.coldRunUpgrade(t, ctx, "namespace-replay")
	f.waitHTTP(t)
	actual, err := os.ReadFile(filepath.Join(f.work, "data", "local-namespace-init", "identity.json"))
	if err != nil || string(actual) != string(body) {
		t.Fatal("initializer did not preserve the exact interrupted identity")
	}
	request, err := ordinaryReadRequestFile(f.request)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(coldCommand(t, ctx, "/usr/bin/docker", "exec", "--user", "postgres", request.DatabaseContainerID, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "houfeng", "-c", `select namespace_id::text from public.local_blob_namespaces where admission_state='active'`)))
	if got != identity.NamespaceID {
		t.Fatal("initializer did not admit the interrupted identity")
	}
}
