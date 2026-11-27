//go:build linux

package deploy

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/recordauthority"
)

// This test runs only in the owned nested Docker host prepared by the runner.
// It exercises real images, the public cold CLI and authenticated HTTP; the
// nested host is not evidence of an upgrade on a separately installed host.
func TestOrdinaryColdHTTPSegment(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Fatal("ordinary cold fixture requires root in its owned Docker host")
	}
	oldImage, targetImage := os.Getenv("HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if !strings.HasPrefix(oldImage, "sha256:") || !strings.HasPrefix(targetImage, "sha256:") || oldImage == targetImage {
		t.Fatal("two distinct immutable fixture images are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, oldImage, targetImage)
	defer f.proxy.Close()
	ordinaryColdAssertC81SourceRefused(t, f)
	f.run(t)
}

// This real HTTP proxy runs in the owned external network and resolves the
// published Compose alias through Docker DNS on every new upstream connection.
func TestOrdinaryColdProxyHelper(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_PROXY_HELPER") != "1" {
		t.Skip("only run inside the owned proxy container")
	}
	upstream, err := url.Parse("http://houfeng:16001")
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal(http.ListenAndServe(":18080", httputil.NewSingleHostReverseProxy(upstream)))
}

func (f *ordinaryColdFixture) assertProxyIngress(t *testing.T, mode string) {
	t.Helper()
	ids := strings.Fields(string(f.compose(t, "ps", "--quiet", "houfeng")))
	if len(ids) != 1 {
		t.Fatal("expected one running Center")
	}
	if mode == "host" {
		binding := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "inspect", "--format", `{{json (index .NetworkSettings.Ports "16001/tcp")}}`, ids[0])))
		var ports []struct {
			HostIP   string
			HostPort string
		}
		if json.Unmarshal([]byte(binding), &ports) != nil || len(ports) != 1 || ports[0].HostIP != "127.0.0.1" || ports[0].HostPort != "16001" {
			t.Fatal("host proxy loopback binding was not preserved")
		}
	} else {
		binding := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "inspect", "--format", `{{json .NetworkSettings.Networks}}`, ids[0])))
		var networks map[string]struct{ Aliases []string }
		if json.Unmarshal([]byte(binding), &networks) != nil {
			t.Fatal("invalid Center networks")
		}
		network, ok := networks[f.proxyNetwork]
		found := false
		for _, alias := range network.Aliases {
			if alias == "houfeng" {
				found = true
			}
		}
		if !ok || !found {
			t.Fatal("external proxy network or houfeng alias was not preserved")
		}
	}
	f.requestHTTP(t, 200, "GET", "/api/healthz", nil, nil)
}

func TestOrdinaryColdProxyOverlays(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	old, target := os.Getenv("HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || old == "" || target == "" || old == target {
		t.Fatal("missing isolated fixture images")
	}
	for _, mode := range []string{"network", "host"} {
		if !t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
			defer cancel()
			f := newOrdinaryColdFixtureMode(t, ctx, old, target, mode)
			defer f.proxy.Close()
			f.assertProxyIngress(t, mode)
			if mode == "network" {
				f.run(t)
			} else {
				f.login(t)
				f.coldRunUpgrade(t, ctx, "proxy-host-upgrade")
				f.waitHTTP(t)
				f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
			}
			f.assertProxyIngress(t, mode)
		}) {
			t.FailNow()
		}
	}
}

func (f *ordinaryColdFixture) coldStackSnapshot(t *testing.T) string {
	t.Helper()
	ids := strings.Fields(string(coldCommand(t, f.ctx, "/usr/bin/docker", "ps", "--all", "--no-trunc", "--quiet", "--filter", "label=com.docker.compose.project="+f.project)))
	if len(ids) == 0 {
		t.Fatal("empty fixture stack")
	}
	sort.Strings(ids)
	var snapshot strings.Builder
	for _, id := range ids {
		data := coldCommand(t, f.ctx, "/usr/bin/docker", "inspect", "--format", `{{.Id}} {{.Image}} {{.State.Status}} {{json .HostConfig.RestartPolicy}} {{json .NetworkSettings.Networks}}`, id)
		snapshot.Write(data)
	}
	return snapshot.String()
}

func (f *ordinaryColdFixture) coldAssertPreflightRefusal(t *testing.T, phase, before string) {
	t.Helper()
	cmd := exec.CommandContext(f.ctx, f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	output, err := cmd.CombinedOutput()
	coldPersistEvidence(t, f.operation+"-"+phase+"-cli.log", output)
	if err == nil || !strings.Contains(string(output), phase) {
		t.Fatalf("expected %s refusal; private output retained", phase)
	}
	if coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json")) != "CLEAN" {
		t.Fatal("refusal did not leave disposition CLEAN")
	}
	if coldExists(filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation, "intent.json")) {
		t.Fatal("refusal published intent")
	}
	if after := f.coldStackSnapshot(t); after != before {
		t.Fatal("refusal changed container identity, status, restart policy, or networks")
	}
}

func TestOrdinaryColdInvalidComposeFileRefused(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, os.Getenv("HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE"))
	defer f.proxy.Close()
	before := f.coldStackSnapshot(t)
	env, err := os.ReadFile(filepath.Join(f.work, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(string(env), "COMPOSE_FILE=compose.yaml:"+f.overlay+"\n", "COMPOSE_FILE=compose.yaml:unsupported.yaml\n", 1)
	if invalid == string(env) {
		t.Fatal("missing selected Compose files")
	}
	coldWrite(t, filepath.Join(f.work, ".env"), []byte(invalid), 0o600)
	f.coldAssertPreflightRefusal(t, "compose-recipe", before)
	coldWrite(t, filepath.Join(f.work, ".env"), env, 0o600)
}

func TestOrdinaryColdWrongC81ChecksumRefused(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, os.Getenv("HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE"))
	defer f.proxy.Close()
	req, err := ordinaryReadRequestFile(f.request)
	if err != nil {
		t.Fatal(err)
	}
	result := strings.TrimSpace(string(coldCommand(t, ctx, "/usr/bin/docker", "exec", "--user", "postgres", req.DatabaseContainerID, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "houfeng", "-c", `update public.schema_migrations set checksum = case when checksum = repeat('0',64) then repeat('1',64) else repeat('0',64) end where name like '0081\_%' escape '\' returning name`)))
	if len(strings.Fields(result)) != 1 {
		t.Fatal("expected exactly one altered C81 checksum")
	}
	before := f.coldStackSnapshot(t)
	f.coldAssertPreflightRefusal(t, "predecessor-catalog", before)
}

type ordinaryColdFixture struct {
	ctx                                                                               context.Context
	work, project, operation, oldImage, targetImage, admin, request, password, origin string
	proxy                                                                             *httptest.Server
	client                                                                            *http.Client
	oldCenterHash, targetCenterHash, targetAdminHash, targetProcessorHash             [32]byte
	overlay, proxyNetwork                                                             string
	previousOperation                                                                 string
}

func coldCommand(t *testing.T, ctx context.Context, args ...string) []byte {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("empty fixture command")
	}
	stepCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(stepCtx, args[0], args[1:]...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/local/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Output can contain credentials or database log detail. Keep it private.
		_ = os.WriteFile("/root/fixture-evidence/command-error.log", out, 0o600)
		if stepCtx.Err() != nil {
			t.Fatalf("fixture command %s exceeded its step deadline: %v (private command-error.log)", filepath.Base(args[0]), stepCtx.Err())
		}
		t.Fatalf("fixture command %s failed: %v (private command-error.log)", filepath.Base(args[0]), err)
	}
	return out
}

func coldRandom(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func coldWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func coldPersistEvidence(t *testing.T, name string, data []byte) {
	t.Helper()
	dir := "/root/fixture-evidence"
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if n, writeErr := file.Write(data); writeErr != nil {
		err = writeErr
	} else if n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	parent, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = parent.Sync()
	closeErr = parent.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func coldCopy(t *testing.T, from, to string, mode os.FileMode) {
	t.Helper()
	body, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	coldWrite(t, to, body, mode)
}

// Cleanup uses a fresh context because the test context may already be cancelled.
// Only resources carrying this fixture's exact labels are removed.
func (f *ordinaryColdFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "/usr/bin/docker", args...)
		cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/local/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
		return cmd.CombinedOutput()
	}
	list := func(kind, label string) []string {
		args := []string{kind, "ls", "--quiet", "--filter", "label=" + label}
		if kind == "container" || kind == "network" {
			args = append(args, "--no-trunc")
		}
		if kind == "container" {
			args = append(args, "--all")
		}
		out, err := run(args...)
		if err != nil {
			t.Errorf("fixture cleanup cannot list %s: %v", kind, err)
			return nil
		}
		return strings.Fields(string(out))
	}
	projectLabel := "com.docker.compose.project=" + f.project
	fixtureLabel := "com.houfeng.fixture=" + f.project
	if t.Failed() {
		if err := os.MkdirAll("/root/fixture-evidence", 0o700); err != nil {
			t.Errorf("cannot create private cleanup evidence: %v", err)
		} else {
			file, err := os.OpenFile(filepath.Join("/root/fixture-evidence", f.operation+"-cleanup-failure.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				t.Errorf("cannot preserve private cleanup evidence: %v", err)
			} else {
				for _, id := range append(list("container", projectLabel), list("container", fixtureLabel)...) {
					for _, args := range [][]string{{"inspect", id}, {"logs", "--tail", "500", id}} {
						out, err := run(args...)
						if _, writeErr := fmt.Fprintf(file, "\n%s %s: %v\n%s\n", args[0], id, err, out); writeErr != nil {
							t.Errorf("cannot write private cleanup evidence: %v", writeErr)
						}
					}
				}
				for _, id := range append(list("network", projectLabel), list("network", fixtureLabel)...) {
					out, inspectErr := run("network", "inspect", id)
					if _, writeErr := fmt.Fprintf(file, "\nnetwork inspect %s: %v\n%s\n", id, inspectErr, out); writeErr != nil {
						t.Errorf("cannot write private network evidence: %v", writeErr)
					}
				}
				if err := file.Sync(); err != nil {
					t.Errorf("cannot sync private cleanup evidence: %v", err)
				}
				if err := file.Close(); err != nil {
					t.Errorf("cannot close private cleanup evidence: %v", err)
				}
			}
		}
	}
	if coldExists(filepath.Join(f.work, ".env")) && coldExists(filepath.Join(f.work, "compose.yaml")) && coldExists(filepath.Join(f.work, f.overlay)) {
		_, err := run("compose", "--project-name", f.project, "--project-directory", f.work, "--file", filepath.Join(f.work, "compose.yaml"), "--file", filepath.Join(f.work, f.overlay), "down", "--timeout", "5", "--volumes", "--remove-orphans")
		if err != nil {
			t.Errorf("fixture Compose teardown failed: %v", err)
		}
	}
	for _, label := range []string{projectLabel, fixtureLabel} {
		ids := list("container", label)
		if len(ids) != 0 {
			if _, err := run(append([]string{"rm", "--force", "--volumes"}, ids...)...); err != nil {
				t.Errorf("fixture container teardown failed: %v", err)
			}
		}
	}
	for _, label := range []string{projectLabel, fixtureLabel} {
		ids := list("network", label)
		if len(ids) != 0 {
			if _, err := run(append([]string{"network", "rm"}, ids...)...); err != nil {
				t.Errorf("fixture network teardown failed: %v", err)
			}
		}
	}
	volumes := list("volume", projectLabel)
	if len(volumes) != 0 {
		if _, err := run(append([]string{"volume", "rm"}, volumes...)...); err != nil {
			t.Errorf("fixture volume teardown failed: %v", err)
		}
	}
}

func newOrdinaryColdFixture(t *testing.T, ctx context.Context, oldImage, targetImage string) *ordinaryColdFixture {
	return newOrdinaryColdFixtureMode(t, ctx, oldImage, targetImage, "")
}

func newOrdinaryColdFixtureMode(t *testing.T, ctx context.Context, oldImage, targetImage, mode string) *ordinaryColdFixture {
	t.Helper()
	if mode == "" {
		mode = "network"
	}
	if mode != "host" && mode != "network" {
		t.Fatal("unknown proxy mode")
	}
	if mode == "host" {
		probe, err := net.Listen("tcp4", "127.0.0.1:16001")
		if err != nil {
			t.Fatalf("fixture host proxy port 127.0.0.1:16001 is already allocated: %v", err)
		}
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
	}
	security := coldCommand(t, ctx, "/usr/bin/docker", "info", "--format", "{{json .SecurityOptions}}")
	if bytes.Contains(bytes.ToLower(security), []byte("rootless")) || bytes.Contains(bytes.ToLower(security), []byte("userns")) {
		t.Fatal("nested daemon is not rootful or uses user namespaces")
	}
	version := strings.TrimSpace(string(coldCommand(t, ctx, "/usr/bin/docker", "compose", "version", "--short")))
	if !ordinaryComposeVersionSupported(version) {
		t.Fatalf("unsupported nested Compose version %q", version)
	}
	t.Logf("nested Compose %s; nested filesystem is not evidence of a real-host Btrfs install", version)
	id := coldRandom(t, 8)
	f := &ordinaryColdFixture{ctx: ctx, work: "/srv/houfeng-fixture-" + id, project: "hfcold" + id, operation: "cold" + id, oldImage: oldImage, targetImage: targetImage, password: coldRandom(t, 24)}
	f.overlay = "compose.proxy-" + mode + ".yaml"
	t.Cleanup(func() { f.cleanup(t) })
	for _, dir := range []string{f.work, f.work + "/data", f.work + "/data/clamav", f.work + "/optional-secrets/comparison-keyring", f.work + "/optional-secrets/s3", "/root/fixture-evidence"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chown(f.work, 0, 10001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.work, 0o750); err != nil {
		t.Fatal(err)
	}
	secretDir := "/root/fixture-secrets-" + id
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.cvd", "daily.cvd", "bytecode.cvd"} {
		coldCopy(t, "/root/fixture-input/clamdb/"+name, f.work+"/data/clamav/"+name, 0o644)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if mode == "host" {
		port = 16001
	}
	if mode == "network" {
		f.proxyNetwork = f.project + "-proxy"
		coldCommand(t, ctx, "/usr/bin/docker", "network", "create", "--label", "com.houfeng.fixture="+f.project, f.proxyNetwork)
		name := f.project + "-proxy-client"
		coldCommand(t, ctx, "/usr/bin/docker", "run", "--detach", "--name", name, "--label", "com.houfeng.fixture="+f.project, "--network", f.proxyNetwork, "--publish", fmt.Sprintf("127.0.0.1:%d:18080", port), "--mount", "type=bind,source=/usr/local/libexec/houfeng-fixture/deploy.test,destination=/fixture.test,readonly", "--env", "HOUFENG_ORDINARY_PROXY_HELPER=1", "--entrypoint", "/fixture.test", "postgres:16.12", "-test.run=^TestOrdinaryColdProxyHelper$", "-test.timeout=45m")
	}
	upstream, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	f.proxy = httptest.NewUnstartedServer(proxy)
	f.proxy.StartTLS()
	f.origin = f.proxy.URL
	roots := x509.NewCertPool()
	roots.AddCert(f.proxy.Certificate())
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	f.client = &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}

	released := oldImage == os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE")
	compose, err := os.ReadFile("/root/fixture-input/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	initialCompose := compose
	if released {
		initialCompose, err = os.ReadFile("/root/fixture-input/released-compose.yaml")
		if err != nil {
			t.Fatal(err)
		}
	}
	type secret struct{ key, variable, value string }
	secrets := []secret{
		{"bootstrap", "POSTGRES_BOOTSTRAP_PASSWORD", coldRandom(t, 24)},
		{"runtime", "HOUFENG_RUNTIME_PASSWORD", coldRandom(t, 24)},
		{"platform_admin", "HOUFENG_PLATFORM_ADMIN_PASSWORD", coldRandom(t, 24)},
		{"deletion_worker", "HOUFENG_RECORD_DELETION_WORKER_PASSWORD", coldRandom(t, 24)},
		{"migrator", "HOUFENG_MIGRATOR_PASSWORD", coldRandom(t, 24)},
		{"initial_password", "HOUFENG_INITIAL_PASSWORD", f.password},
		{"session_hmac_key", "HOUFENG_SESSION_HMAC_KEY", coldRandom(t, 32)},
	}
	paths := make(map[string]string, len(secrets))
	secretEnv := ""
	for _, s := range secrets {
		input := secretDir + "/input-" + s.key
		coldWrite(t, input, []byte(s.value), 0o600)
		paths[s.key] = input
		secretEnv += s.variable + "=" + s.value + "\n"
	}
	coldWrite(t, f.work+"/compose.yaml", initialCompose, 0o644)
	for _, overlay := range []string{"compose.proxy-host.yaml", "compose.proxy-network.yaml"} {
		source := "/root/fixture-input/" + overlay
		if released {
			source = "/root/fixture-input/released-" + overlay
		}
		coldCopy(t, source, f.work+"/"+overlay, 0o644)
	}
	f.overlay = "compose.proxy-" + mode + ".yaml"
	env := fmt.Sprintf("COMPOSE_PROJECT_NAME=%s\nHOUFENG_IMAGE=%s\nHOUFENG_PUBLIC_BASE_URL=%s\nHOUFENG_INITIAL_USERNAME=coldfixture\n", f.project, oldImage, f.origin) + secretEnv
	if f.overlay != "" {
		env += "COMPOSE_FILE=compose.yaml:" + f.overlay + "\n"
	}
	if f.proxyNetwork != "" {
		env += "HOUFENG_PROXY_NETWORK=" + f.proxyNetwork + "\n"
	}
	coldWrite(t, f.work+"/.env", []byte(env), 0o600)
	f.admin = f.work + "/controller-admin"
	f.oldCenterHash = f.extractImageBinary(t, oldImage, "houfeng-center", f.work+"/old-center")
	f.targetCenterHash = f.extractImageBinary(t, targetImage, "houfeng-center", f.work+"/target-center")
	if f.oldCenterHash == f.targetCenterHash {
		t.Fatal("different Center builds produced the same executable")
	}
	if released {
		f.extractImageBinary(t, oldImage, "houfeng-record-platform-admin", f.work+"/released-admin")
		f.extractImageBinary(t, targetImage, "houfeng-record-platform-admin", f.admin)
	} else {
		f.extractImageBinary(t, oldImage, "houfeng-record-platform-admin", f.admin)
	}
	f.targetAdminHash = f.extractImageBinary(t, targetImage, "houfeng-record-platform-admin", f.work+"/target-admin")
	oldProcessor := f.extractImageBinary(t, oldImage, "houfeng-content-processor", f.work+"/old-processor")
	f.targetProcessorHash = f.extractImageBinary(t, targetImage, "houfeng-content-processor", f.work+"/target-processor")
	targetProcessor := f.targetProcessorHash
	if released {
		t.Log("released v1.27.5 producer image; final C81 is checked on the target")
	} else if oldImage == os.Getenv("HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE") {
		if oldProcessor != targetProcessor {
			t.Fatal("plain production processor unexpectedly changed")
		}
		t.Log("plain production Dockerfile pair: Center differs; processor remains identical")
	} else {
		if oldProcessor == targetProcessor {
			t.Fatal("mixed-file pair needs distinct actual processor bytes")
		}
		t.Log("production Dockerfile plus same-source processor with distinct build ID: Center and processor differ")
	}
	targetContract := f.probeContract(t, f.work+"/target-admin")
	if !released && !bytes.Equal(f.probeContract(t, f.admin), targetContract) {
		t.Fatal("old and target binaries do not embed the same C81 contract")
	}
	var contract ordinaryTargetContract
	if json.Unmarshal(targetContract, &contract) != nil || contract.SchemaVersion != 81 {
		t.Fatal("expected final schema C81")
	}
	t.Logf("image IDs old=%s target=%s; center SHA256 old=%x target=%x; processor SHA256 old=%x target=%x; target C81=%s", oldImage, targetImage, f.oldCenterHash, f.targetCenterHash, oldProcessor, targetProcessor, contract.SourceIdentitySHA256)
	f.compose(t, "up", "-d")
	f.waitHTTP(t)
	if released {
		coldWrite(t, f.work+"/compose.yaml", []byte(compose), 0o644)
		for _, overlay := range []string{"compose.proxy-host.yaml", "compose.proxy-network.yaml"} {
			coldCopy(t, "/root/fixture-input/"+overlay, f.work+"/"+overlay, 0o644)
		}
	}
	dbIDs := strings.Fields(string(f.compose(t, "ps", "--all", "--quiet", "db")))
	if len(dbIDs) != 1 {
		t.Fatal("expected exactly one database container")
	}
	db := strings.TrimSpace(string(coldCommand(t, ctx, "/usr/bin/docker", "inspect", "--format", "{{.Id}}", dbIDs[0])))
	if !fenceID.MatchString(db) {
		t.Fatal("expected one full database container ID")
	}
	req := ordinaryUpgradeRequest{Version: 1, OperationID: f.operation, Project: f.project, WorkingDirectory: f.work, DatabaseContainerID: db, TargetImageID: targetImage, Secrets: ordinaryUpgradeSecrets{Bootstrap: paths["bootstrap"], Runtime: paths["runtime"], PlatformAdmin: paths["platform_admin"], DeletionWorker: paths["deletion_worker"], Migrator: paths["migrator"], InitialPassword: paths["initial_password"], SessionHMACKey: paths["session_hmac_key"]}}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	f.request = secretDir + "/request.json"
	coldWrite(t, f.request, append(body, '\n'), 0o600)
	return f
}

func (f *ordinaryColdFixture) compose(t *testing.T, args ...string) []byte {
	t.Helper()
	stepCtx, cancel := context.WithTimeout(f.ctx, 6*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(stepCtx, "/usr/bin/docker", append([]string{"compose"}, args...)...)
	cmd.Dir = f.work
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/local/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.WriteFile("/root/fixture-evidence/compose-command-error.log", out, 0o600)
		t.Fatalf("fixture Compose command failed: %v (private compose-command-error.log)", err)
	}
	return out
}

func (f *ordinaryColdFixture) extractImageBinary(t *testing.T, image, name, dest string) [32]byte {
	t.Helper()
	id := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "create", "--network", "none", "--entrypoint", "/bin/false", image)))
	if !fenceID.MatchString(id) {
		t.Fatal("invalid image extraction container ID")
	}
	// Read ownership and mode from the daemon's archive header itself; local
	// extraction need not preserve the image's numeric owner.
	copyCmd := exec.CommandContext(f.ctx, "/usr/bin/docker", "cp", id+":/usr/local/bin/"+name, "-")
	copyCmd.Env = []string{"PATH=/usr/bin:/bin:/usr/local/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	archive, copyErr := copyCmd.Output()
	coldCommand(t, f.ctx, "/usr/bin/docker", "rm", id)
	if copyErr != nil {
		t.Fatalf("image %s executable %s archive copy failed: %v", image, name, copyErr)
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	header, err := reader.Next()
	if err != nil || header.Name != name || header.Typeflag != tar.TypeReg || header.Linkname != "" {
		t.Fatalf("image %s executable %s is not one regular archive entry", image, name)
	}
	if header.Uid != 10001 || header.Gid != 10001 || header.Mode&0o7777 != 0o755 {
		t.Fatalf("image %s executable %s owner/mode %d:%d %#o differs from production 10001:10001 0755", image, name, header.Uid, header.Gid, header.Mode&0o7777)
	}
	body, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
	if err != nil || int64(len(body)) != header.Size {
		t.Fatalf("image %s executable %s archive body is truncated", image, name)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("image %s executable %s archive has extra entries", image, name)
	}
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := output.Chown(0, 10001); err != nil {
		t.Fatal(err)
	}
	if err := output.Chmod(0o550); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	var imageStat unix.Stat_t
	if unix.Lstat(dest, &imageStat) != nil || imageStat.Size != header.Size {
		t.Fatal("extracted image executable changed")
	}
	file, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := ordinaryStaticELF(file, imageStat.Size, runtime.GOARCH); err != nil {
		t.Fatal("image executable is not a supported static ELF")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

func (f *ordinaryColdFixture) probeContract(t *testing.T, path string) []byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	// Use the production two-exec probe path; a hand-built launcher would not
	// satisfy its parent, stdio and descriptor-role preconditions.
	probeCtx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	out, err := ordinaryProbeTargetContract(probeCtx, path, digest)
	if err != nil {
		t.Fatalf("actual image contract probe failed: %v", err)
	}
	if _, err := ordinaryDecodeTargetContract(out); err != nil {
		t.Fatal("invalid embedded image contract")
	}
	return out
}

func (f *ordinaryColdFixture) waitHTTP(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		response, err := f.client.Get(f.origin + "/api/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-time.After(time.Second):
		}
	}
	t.Fatal("actual Center did not become ready")
}

func (f *ordinaryColdFixture) requestHTTP(t *testing.T, want int, method, path string, body any, headers map[string]string) []byte {
	t.Helper()
	var data io.Reader
	switch value := body.(type) {
	case nil:
	case []byte:
		data = bytes.NewReader(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(f.ctx, method, f.origin+path, data)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", f.origin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("HTTP %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("HTTP %s %s: got %d want %d", method, path, res.StatusCode, want)
	}
	return out
}

func coldJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if json.Unmarshal(body, &value) != nil {
		t.Fatal("invalid HTTP JSON")
	}
	return value
}

func coldString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	value, ok := m[key].(string)
	if !ok || value == "" {
		t.Fatalf("missing HTTP field %s", key)
	}
	return value
}

func (f *ordinaryColdFixture) login(t *testing.T) {
	f.requestHTTP(t, 200, "POST", "/api/auth/login", map[string]any{"username": "coldfixture", "password": f.password}, nil)
	origin, _ := url.Parse(f.origin)
	if len(f.client.Jar.Cookies(origin)) == 0 {
		t.Fatal("real login did not issue a session cookie")
	}
	f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
}

func (f *ordinaryColdFixture) payload(subject, content string, attachments []string) map[string]any {
	return map[string]any{"title": "ordinary cold fixture", "body_markdown": content, "markdown_dialect_version": 1, "record_type": "note", "business_status": "", "impact_level": "low", "visibility": map[string]any{"kind": "project", "allowed_roles": []string{}, "allowed_group_ids": []string{}}, "subjects": []any{map[string]any{"registry_version": 1, "kind": "vps", "role": "affected", "source_id": subject, "primary": true}}, "tags": []string{}, "owner_id": "", "participant_ids": []string{}, "attachment_ids": attachments, "save_reason": "ordinary cold fixture"}
}

func (f *ordinaryColdFixture) run(t *testing.T) {
	f.login(t)
	subject := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/vps", map[string]any{"display_name": "cold fixture source", "lifecycle_status": "active"}, nil))
	subjectID := coldString(t, subject, "vps_id")
	oldBody := "# Existing real content\n" + coldRandom(t, 16) + "\n"
	payload := f.payload(subjectID, oldBody, []string{})
	draft := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/record-drafts", map[string]any{"payload": payload}, nil))
	created := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/records", map[string]any{"draft_id": coldString(t, draft, "draft_id"), "draft_etag": coldString(t, draft, "etag")}, map[string]string{"Idempotency-Key": "ordinary-cold-old"}))
	recordID := coldString(t, created, "record_id")
	before := coldJSON(t, f.requestHTTP(t, 200, "GET", "/api/records/"+recordID, nil, nil))
	current, ok := before["current"].(map[string]any)
	if !ok || coldString(t, current, "body_markdown") != oldBody {
		t.Fatal("old record did not contain the created content")
	}
	oldHash := sha256.Sum256([]byte(oldBody))
	upgradeCtx, cancel := context.WithTimeout(f.ctx, 8*time.Minute)
	defer cancel()
	f.coldRunUpgrade(t, upgradeCtx, "http-upgrade")
	f.waitHTTP(t)
	// The existing session must remain valid; then prove a fresh login too.
	f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
	f.login(t)
	after := coldJSON(t, f.requestHTTP(t, 200, "GET", "/api/records/"+recordID, nil, nil))
	current, ok = after["current"].(map[string]any)
	if !ok || sha256.Sum256([]byte(coldString(t, current, "body_markdown"))) != oldHash {
		t.Fatal("old record content changed after cold upgrade")
	}
	for _, route := range []string{"permanent-delete-preview", "permanent-delete"} {
		body := coldJSON(t, f.requestHTTP(t, 503, "POST", "/api/records/"+recordID+"/"+route, map[string]any{}, nil))
		if coldString(t, body, "code") != "deletion_safety_unavailable" {
			t.Fatal("unexpected permanent-delete closure")
		}
	}
	newBody := "# New revision after cold upgrade\n" + coldRandom(t, 16) + "\n"
	edit := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/record-drafts", map[string]any{"record_id": recordID, "base_revision_id": coldString(t, after, "current_revision_id"), "payload": f.payload(subjectID, newBody, []string{})}, nil))
	f.requestHTTP(t, 201, "POST", "/api/records/"+recordID+"/revisions", map[string]any{"draft_id": coldString(t, edit, "draft_id"), "draft_etag": coldString(t, edit, "etag"), "base_revision_id": coldString(t, after, "current_revision_id"), "lock_version": after["lock_version"], "authorization_epoch": after["authorization_epoch"]}, map[string]string{"Idempotency-Key": "ordinary-cold-new"})
	latest := coldJSON(t, f.requestHTTP(t, 200, "GET", "/api/records/"+recordID, nil, nil))
	latestCurrent, ok := latest["current"].(map[string]any)
	if !ok || coldString(t, latestCurrent, "body_markdown") != newBody {
		t.Fatal("new authenticated revision was not readable")
	}
	// Prevent a segment-only result from being mistaken for full acceptance.
	f.verifyColdWorkloadAndRecovery(t, recordID, subjectID, latest)
}

// Permission marks cover only the fixture directories being observed.
func coldFanotify(t *testing.T, paths ...string) int {
	t.Helper()
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK, unix.O_RDONLY|unix.O_LARGEFILE|unix.O_CLOEXEC)
	if err != nil {
		t.Fatalf("fanotify permission events unavailable: %v", err)
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if err := unix.FanotifyMark(fd, unix.FAN_MARK_ADD, unix.FAN_OPEN_PERM|unix.FAN_EVENT_ON_CHILD, unix.AT_FDCWD, path); err != nil {
			unix.Close(fd)
			t.Fatalf("fanotify directory permission mark unavailable: %v", err)
		}
	}
	return fd
}

func (f *ordinaryColdFixture) verifyColdWorkloadAndRecovery(t *testing.T, recordID, subjectID string, latest map[string]any) {
	t.Helper()
	var request ordinaryUpgradeRequest
	requestBody, err := os.ReadFile(f.request)
	if err != nil || json.Unmarshal(requestBody, &request) != nil {
		t.Fatal("cannot read fixture request")
	}
	psql := func(sql string) string {
		return strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "exec", "--user", "postgres", request.DatabaseContainerID, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "houfeng", "-c", sql)))
	}
	psql("ALTER SYSTEM SET log_statement = 'all'")
	if psql("SELECT pg_reload_conf()") != "t" {
		t.Fatal("could not enable private fixture PostgreSQL statement evidence")
	}
	imageValue := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageValue.Set(0, 0, color.RGBA{R: 0x52, G: 0x91, B: 0x40, A: 0xff})
	imageValue.Set(1, 1, color.RGBA{R: 0x22, G: 0x71, B: 0xa0, A: 0xff})
	var pngBody bytes.Buffer
	if err := png.Encode(&pngBody, imageValue); err != nil {
		t.Fatal(err)
	}
	content := pngBody.Bytes()
	digest := sha256.Sum256(content)
	edit := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/record-drafts", map[string]any{"record_id": recordID, "base_revision_id": coldString(t, latest, "current_revision_id"), "payload": f.payload(subjectID, "# Processor workload\n", []string{})}, nil))
	draftID := coldString(t, edit, "draft_id")
	upload := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/attachment-uploads", map[string]any{"draft_id": draftID, "display_name": "cold.png", "media_type": "image/png", "declared_size_bytes": len(content)}, nil))
	uploadID, attachmentID := coldString(t, upload, "upload_id"), coldString(t, upload, "attachment_id")
	if strings.Trim(uploadID, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" || strings.Trim(attachmentID, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
		t.Fatal("unexpected upload identity")
	}
	f.requestHTTP(t, 200, "PUT", "/api/attachment-uploads/"+uploadID+"/content", content, map[string]string{"Content-Type": "application/octet-stream", "X-Houfeng-Draft-ID": draftID, "X-Content-SHA256": hex.EncodeToString(digest[:])})
	f.requestHTTP(t, 202, "POST", "/api/attachment-uploads/"+uploadID+"/complete", map[string]any{"draft_id": draftID}, nil)
	deadline := time.Now().Add(3 * time.Minute)
	for {
		metadata := coldJSON(t, f.requestHTTP(t, 200, "GET", "/api/attachments/"+attachmentID, nil, nil))
		state := coldString(t, metadata, "state")
		if state == "available" {
			if metadata["preview_available"] != true {
				t.Fatal("actual processor did not make a preview")
			}
			break
		}
		if state == "rejected" || state == "expired" || time.Now().After(deadline) {
			t.Fatalf("processor did not complete: %s", state)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !bytes.Equal(f.requestHTTP(t, 200, "GET", "/api/attachments/"+attachmentID+"/content", nil, nil), content) {
		t.Fatal("actual attachment bytes changed")
	}
	row := psql("SELECT processor_job_id || '|' || processor_state || '|' || result_owner_id FROM public.attachment_processor_jobs WHERE attachment_id='" + attachmentID + "'")
	parts := strings.Split(row, "|")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "apj_") || parts[1] != "succeeded" || parts[2] != "compose-processor" {
		t.Fatal("processor job did not succeed under the selected worker")
	}
	if psql("SELECT count(*) FROM public.deployment_membership WHERE instance_id IN ('compose-center','compose-processor') AND deployment_epoch=2") != "2" || psql("SELECT count(*) FROM public.deployment_membership WHERE deployment_epoch=1") != "0" {
		t.Fatal("worker membership epoch is not the selected epoch 2")
	}
	logs := coldCommand(t, f.ctx, "/usr/bin/docker", "logs", request.DatabaseContainerID)
	coldWrite(t, "/root/fixture-evidence/postgres-statements.log", logs, 0o600)
	claimed, renewed, completed := false, false, false
	for _, entry := range bytes.Split(logs, []byte("LOG:")) {
		if bytes.Contains(entry, []byte("set processor_state = 'claimed'")) && bytes.Contains(entry, []byte("$3 = '"+parts[0]+"'")) && bytes.Contains(entry, []byte("$1 = 'compose-processor'")) {
			claimed = true
		}
		if bytes.Contains(entry, []byte("update public.attachment_processor_jobs as job")) && bytes.Contains(entry, []byte("set lease_expires_at = least")) && bytes.Contains(entry, []byte("$1 = '"+parts[0]+"'")) {
			renewed = true
		}
		if bytes.Contains(entry, []byte("set processor_state = $6")) && bytes.Contains(entry, []byte("$1 = '"+parts[0]+"'")) && bytes.Contains(entry, []byte("$10 = 'compose-processor'")) {
			completed = true
		}
	}
	if !claimed || !renewed || !completed {
		t.Fatal("private PostgreSQL log did not correlate claim, renewal and completion for the real job")
	}
	edit = coldJSON(t, f.requestHTTP(t, 200, "PATCH", "/api/record-drafts/"+draftID, map[string]any{"payload": f.payload(subjectID, "# Processor workload\n", []string{attachmentID})}, map[string]string{"If-Match": coldString(t, edit, "etag")}))
	f.requestHTTP(t, 201, "POST", "/api/records/"+recordID+"/revisions", map[string]any{"draft_id": draftID, "draft_etag": coldString(t, edit, "etag"), "base_revision_id": coldString(t, latest, "current_revision_id"), "lock_version": latest["lock_version"], "authorization_epoch": latest["authorization_epoch"]}, map[string]string{"Idempotency-Key": "ordinary-cold-processor"})
	intentPath := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation, "intent.json")
	intentBody, err := os.ReadFile(intentPath)
	var intent ordinaryUpgradeIntent
	if err != nil || json.Unmarshal(intentBody, &intent) != nil || ordinaryValidateIntent(intent) != nil {
		t.Fatal("durable intent missing or invalid")
	}
	if intent.Request.TargetImageID != f.targetImage || intent.Target.CenterSHA256 != hex.EncodeToString(f.targetCenterHash[:]) || intent.Target.AdminSHA256 != hex.EncodeToString(f.targetAdminHash[:]) || intent.Target.ProcessorSHA256 != hex.EncodeToString(f.targetProcessorHash[:]) {
		t.Fatal("durable target identity differs from actual image executables")
	}
	if _, err := ordinaryVerifySelectedProcesses(f.ctx, intent); err != nil {
		t.Fatal("selected target executable or runtime identity was not proven")
	}
	db, err := fenceInspect(f.ctx, request.DatabaseContainerID)
	if err != nil {
		t.Fatal("cannot inspect restored database")
	}
	var original fenceContainer
	for _, old := range intent.Fence.Containers {
		if old.ID == request.DatabaseContainerID {
			original = old
		}
	}
	if original.ID == "" || !fenceAllowedNetworks(original, db, "", false) {
		t.Fatal("database networks and aliases were not restored")
	}
	if db.anchor.Restart != original.Restart {
		t.Fatal("database restart policy differs from the recorded predecessor")
	}
	for _, old := range intent.Fence.Containers {
		if old.ID == request.DatabaseContainerID {
			continue
		}
		// Exact existence query; do not parse the daemon's not-found wording.
		present := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "ps", "--all", "--quiet", "--no-trunc", "--filter", "id="+old.ID)))
		if present == "" {
			continue
		}
		if present != old.ID {
			t.Fatal("old container identity query returned an unexpected container")
		}
		state := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "container", "inspect", "--format", "{{.State.Running}}|{{.HostConfig.RestartPolicy.Name}}", old.ID)))
		if state != "false|no" {
			t.Fatalf("old container %s remained running or restart enabled: %s", old.ID[:12], state)
		}
	}
	if err := ordinaryVerifyComposeRuntime(f.ctx, intent); err != nil {
		t.Fatal("selected services or their recipe restart policies were not proven")
	}
}

type coldPermissionEvent struct {
	fd, pid int
	path    string
	serial  uint64
}

func coldPermissionReply(group int, event coldPermissionEvent, decision uint32) error {
	var response [8]byte
	binary.NativeEndian.PutUint32(response[0:4], uint32(event.fd))
	binary.NativeEndian.PutUint32(response[4:8], decision)
	_, err := unix.Write(group, response[:])
	_ = unix.Close(event.fd)
	return err
}

func coldDescendant(pid, ancestor int) bool {
	for i := 0; i < 64 && pid > 1; i++ {
		if pid == ancestor {
			return true
		}
		body, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return false
		}
		next := 0
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				next, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
				break
			}
		}
		if next == pid {
			return false
		}
		pid = next
	}
	return false
}

type coldPermissionDecision struct {
	event        coldPermissionEvent
	decision     uint32
	childFD      int
	controllerFD int
	ack          chan error
}

type coldPermissionWatcher struct {
	stop            chan struct{}
	done            chan struct{}
	faults          chan error
	events          chan coldPermissionEvent
	decisions       chan coldPermissionDecision
	childPID        atomic.Int64
	childFD         atomic.Int64
	namespaceRoot   string
	signalDelivered atomic.Bool
	once            sync.Once
}

func (w *coldPermissionWatcher) stopWatching() { w.once.Do(func() { close(w.stop) }) }

// Only this goroutine reads the fanotify queue and writes permission replies.
// It never opens a path, and releases every outstanding event on every exit.
func (w *coldPermissionWatcher) run(group, rootPID int) {
	type held struct {
		event        coldPermissionEvent
		deadline     time.Time
		ack          chan error
		childFD      int
		controllerFD int
	}
	pending := make(map[int]held)
	var serial uint64
	notify := func(err error) {
		select {
		case w.faults <- err:
		default:
		}
	}
	defer func() {
		for _, item := range pending {
			_ = coldPermissionReply(group, item.event, unix.FAN_ALLOW)
			if item.ack != nil {
				item.ack <- errors.New("fanotify listener stopped before denial")
			}
		}
		_ = unix.Close(group)
		close(w.done)
	}()
	buffer := make([]byte, 64<<10)
	for {
		for {
			select {
			case <-w.stop:
				return
			case response := <-w.decisions:
				item, ok := pending[response.event.fd]
				if !ok || item.event.serial != response.event.serial {
					response.ack <- errors.New("permission event already released")
					continue
				}
				if item.ack != nil {
					response.ack <- errors.New("permission event already awaiting exit")
					continue
				}
				if response.controllerFD > 0 {
					if response.decision != unix.FAN_DENY || !time.Now().Before(item.deadline) {
						response.ack <- errors.New("permission event expired before pidfd signal")
						notify(errors.New("permission event expired before pidfd signal"))
						return
					}
					selectedFD := response.controllerFD
					if w.namespaceRoot == "" && item.event.pid != rootPID {
						selectedFD = response.childFD
					}
					if selectedFD <= 0 {
						response.ack <- errors.New("held actor has no pinned pidfd")
						notify(errors.New("held actor has no pinned pidfd"))
						return
					}
					for _, signalFD := range []int{response.childFD, response.controllerFD} {
						if signalFD <= 0 {
							continue
						}
						err := unix.PidfdSendSignal(signalFD, unix.SIGKILL, nil, 0)
						if err == nil && w.namespaceRoot != "" && signalFD == selectedFD {
							w.signalDelivered.Store(true)
						}
						if err != nil && (!errors.Is(err, unix.ESRCH) || signalFD == selectedFD) {
							response.ack <- err
							notify(fmt.Errorf("pidfd termination: %w", err))
							return
						}
					}
					item.ack, item.childFD, item.controllerFD = response.ack, response.childFD, response.controllerFD
					pending[response.event.fd] = item
					continue
				}
				delete(pending, response.event.fd)
				response.ack <- coldPermissionReply(group, item.event, response.decision)
			default:
				goto drained
			}
		}
	drained:
		for fd, item := range pending {
			if !time.Now().Before(item.deadline) {
				_ = coldPermissionReply(group, item.event, unix.FAN_ALLOW)
				delete(pending, fd)
				if item.ack != nil {
					item.ack <- errors.New("pidfd exit was not observed before the event deadline")
				}
				notify(errors.New("fanotify permission event exceeded its 20 second deadline"))
				return
			}
			if item.ack != nil {
				poll := []unix.PollFd{{Fd: int32(item.controllerFD), Events: unix.POLLIN}}
				if item.childFD > 0 {
					poll = append(poll, unix.PollFd{Fd: int32(item.childFD), Events: unix.POLLIN})
				}
				if _, err := unix.Poll(poll, 0); err != nil && !errors.Is(err, unix.EINTR) {
					notify(err)
					return
				}
				exited := true
				for _, descriptor := range poll {
					if descriptor.Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
						notify(errors.New("pidfd exit observation failed"))
						return
					}
					if descriptor.Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
						exited = false
					}
				}
				if exited {
					delete(pending, fd)
					err := coldPermissionReply(group, item.event, unix.FAN_DENY)
					item.ack <- err
					if err != nil {
						notify(err)
						return
					}
				}
			}
		}
		poll := []unix.PollFd{{Fd: int32(group), Events: unix.POLLIN}}
		if _, err := unix.Poll(poll, 25); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			notify(err)
			return
		}
		if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			notify(errors.New("fanotify listener failed"))
			return
		}
		if poll[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err := unix.Read(group, buffer)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			notify(err)
			return
		}
		for offset := 0; offset+24 <= n; {
			length := int(binary.NativeEndian.Uint32(buffer[offset:]))
			if length < 24 || offset+length > n {
				notify(errors.New("malformed fanotify event"))
				return
			}
			mask := binary.NativeEndian.Uint64(buffer[offset+8:])
			fd := int(int32(binary.NativeEndian.Uint32(buffer[offset+16:])))
			pid := int(int32(binary.NativeEndian.Uint32(buffer[offset+20:])))
			offset += length
			if fd < 0 {
				if mask&unix.FAN_Q_OVERFLOW != 0 {
					notify(errors.New("fanotify queue overflow"))
					return
				}
				continue
			}
			event := coldPermissionEvent{fd: fd, pid: pid}
			selected := pid == rootPID || int64(pid) == w.childPID.Load()
			if w.namespaceRoot != "" {
				path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
				selected = err == nil && strings.HasPrefix(filepath.Base(path), ".houfeng-local-namespace.tmp-")
			}
			if mask&unix.FAN_OPEN_PERM == 0 || !selected {
				if err := coldPermissionReply(group, event, unix.FAN_ALLOW); err != nil {
					notify(err)
					return
				}
				continue
			}
			serial++
			event.serial = serial
			pending[fd] = held{event: event, deadline: time.Now().Add(20 * time.Second)}
			select {
			case w.events <- event:
			default:
				_ = coldPermissionReply(group, event, unix.FAN_ALLOW)
				delete(pending, fd)
				notify(errors.New("fanotify coordinator queue full"))
				return
			}
		}
	}
}

func (w *coldPermissionWatcher) reply(ctx context.Context, event coldPermissionEvent, decision uint32) error {
	ack := make(chan error, 1)
	select {
	case w.decisions <- coldPermissionDecision{event: event, decision: decision, ack: ack}:
	case <-w.done:
		return errors.New("fanotify listener stopped")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-ack:
		return err
	case <-w.done:
		return errors.New("fanotify listener stopped")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *coldPermissionWatcher) killAndDeny(ctx context.Context, event coldPermissionEvent, childFD, controllerFD int) error {
	ack := make(chan error, 1)
	request := coldPermissionDecision{event: event, decision: unix.FAN_DENY, childFD: childFD, controllerFD: controllerFD, ack: ack}
	select {
	case w.decisions <- request:
	case <-w.done:
		return errors.New("fanotify listener stopped before pidfd termination")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-ack:
		return err
	case <-w.done:
		select {
		case err := <-ack:
			return err
		default:
			return errors.New("fanotify listener stopped before denial")
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func coldExists(path string) bool { _, err := os.Lstat(path); return err == nil }

func coldTemps(dir string) map[string]bool {
	result := make(map[string]bool)
	for _, pattern := range []string{".ordinary-launch-*", ".birth-launch-*", ".tmp-*", ".membership.tmp-*"} {
		paths, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, path := range paths {
			result[path] = true
		}
	}
	return result
}

type coldBoundaryEntry struct {
	Name   string `json:"name"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Mode   uint32 `json:"mode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Links  uint64 `json:"links"`
	Size   int64  `json:"size"`
}

func coldBoundaryEntryFromStat(name string, st unix.Stat_t) coldBoundaryEntry {
	return coldBoundaryEntry{Name: name, Device: uint64(st.Dev), Inode: st.Ino, Mode: st.Mode, UID: st.Uid, GID: st.Gid, Links: uint64(st.Nlink), Size: st.Size}
}

func coldBoundaryEntries(t *testing.T, directory string) []coldBoundaryEntry {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]coldBoundaryEntry, 0, len(entries))
	for _, entry := range entries {
		var st unix.Stat_t
		if err := unix.Lstat(filepath.Join(directory, entry.Name()), &st); err != nil {
			t.Fatal(err)
		}
		result = append(result, coldBoundaryEntryFromStat(entry.Name(), st))
	}
	return result
}

func coldBoundaryExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatal(err)
	return false
}

// The durable observation is written before killing and again before retry,
// so later retry effects cannot be mistaken for the interrupted boundary.
func (f *ordinaryColdFixture) coldBoundaryWitness(t *testing.T, stage, phase, parent string, event coldPermissionEvent, held unix.Stat_t, controllerPID, firstTempOpens int, stagedRevision string) {
	t.Helper()
	op := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	var named unix.Stat_t
	if err := unix.Lstat(event.path, &named); err != nil || named.Dev != held.Dev || named.Ino != held.Ino || named.Mode&unix.S_IFMT != unix.S_IFREG || named.Size != 0 || named.Nlink != 1 {
		t.Fatal("held temporary inode changed at the boundary")
	}
	operationEntries := coldBoundaryEntries(t, op)
	parentEntries := operationEntries
	if parent != op {
		parentEntries = coldBoundaryEntries(t, parent)
	}
	disposition := coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json"))
	intentPresent := coldBoundaryExists(t, filepath.Join(op, "intent.json"))
	fencePath := filepath.Join(f.work, ".ordinary-fences", f.operation, "fences")
	fencePresent := coldBoundaryExists(t, filepath.Join(fencePath, f.operation+".json"))
	networkPresent := coldBoundaryExists(t, filepath.Join(fencePath, f.operation+".network.json"))
	participants, err := filepath.Glob(filepath.Join(op, "participant-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stage == "intent" || stage == "override" {
		if firstTempOpens != 1 || event.pid != controllerPID || filepath.Dir(event.path) != op || !ordinaryOperationTemporaryName.MatchString(filepath.Base(event.path)) || named.Uid != 0 || named.Gid != 0 || named.Mode&0o7777 != 0o600 || disposition != "CLEAN" || intentPresent || fencePresent || networkPresent || len(participants) != 0 {
			t.Fatal("first eligible temporary open is not the requested CLEAN boundary")
		}
		foundHeld, foundOverride := false, false
		for _, entry := range operationEntries {
			switch entry.Name {
			case "backends":
			case "compose-override.yaml":
				foundOverride = true
			case filepath.Base(event.path):
				foundHeld = entry.Device == uint64(held.Dev) && entry.Inode == held.Ino
			default:
				t.Fatal("unexpected operation entry at CLEAN boundary")
			}
		}
		if !foundHeld || foundOverride != (stage == "intent") {
			t.Fatal("operation entries do not match the requested boundary")
		}
		if foundOverride {
			request, requestErr := ordinaryReadRequestFile(f.request)
			expected, expectedErr := ordinaryComposeOverride(request)
			actual, readErr := os.ReadFile(filepath.Join(op, "compose-override.yaml"))
			if requestErr != nil || expectedErr != nil || readErr != nil || !bytes.Equal(actual, expected) {
				t.Fatal("published override differs from canonical request")
			}
		}
	}
	document := struct {
		Version                int                 `json:"version"`
		Stage                  string              `json:"stage"`
		Phase                  string              `json:"phase"`
		Operation              string              `json:"operation"`
		ObservedAt             time.Time           `json:"observed_at"`
		ControllerPID          int                 `json:"controller_pid"`
		EventPID               int                 `json:"event_pid"`
		EventSerial            uint64              `json:"event_serial"`
		ExitObservedBeforeDeny bool                `json:"exit_observed_before_deny"`
		FirstTempOpens         int                 `json:"first_temp_opens"`
		HeldPath               string              `json:"held_path"`
		Held                   coldBoundaryEntry   `json:"held"`
		Disposition            string              `json:"disposition"`
		IntentPresent          bool                `json:"intent_present"`
		FenceJournalPresent    bool                `json:"fence_journal_present"`
		FenceNetworkPresent    bool                `json:"fence_network_present"`
		Participants           []string            `json:"participants"`
		StagedRevision         string              `json:"staged_revision"`
		OperationEntries       []coldBoundaryEntry `json:"operation_entries"`
		ParentEntries          []coldBoundaryEntry `json:"parent_entries"`
	}{1, stage, phase, f.operation, time.Now().UTC(), controllerPID, event.pid, event.serial, phase == "pre-retry", firstTempOpens, event.path, coldBoundaryEntryFromStat(filepath.Base(event.path), named), disposition, intentPresent, fencePresent, networkPresent, participants, stagedRevision, operationEntries, parentEntries}
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	coldPersistEvidence(t, f.operation+"-"+stage+"-"+phase+".json", append(body, '\n'))
}

func (f *ordinaryColdFixture) coldIntent(t *testing.T) ordinaryUpgradeIntent {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation, "intent.json"))
	var intent ordinaryUpgradeIntent
	if err != nil || json.Unmarshal(body, &intent) != nil || ordinaryValidateIntent(intent) != nil {
		t.Fatal("durable intent is invalid")
	}
	return intent
}

func (f *ordinaryColdFixture) coldRestoredNetwork(t *testing.T) {
	t.Helper()
	intent := f.coldIntent(t)
	db, err := fenceInspect(f.ctx, intent.Request.DatabaseContainerID)
	if err != nil {
		t.Fatal("cannot observe restored DB network")
	}
	for _, original := range intent.Fence.Containers {
		if original.ID == intent.Request.DatabaseContainerID {
			if !fenceAllowedNetworks(original, db, "", false) {
				t.Fatal("original DB networks and aliases are not restored")
			}
			return
		}
	}
	t.Fatal("intent has no original DB container")
}

func (f *ordinaryColdFixture) coldOldContainersStopped(t *testing.T) {
	t.Helper()
	intent := f.coldIntent(t)
	for _, original := range intent.Fence.Containers {
		if original.ID == intent.Request.DatabaseContainerID {
			continue
		}
		observed, err := fenceInspect(f.ctx, original.ID)
		if err != nil || (observed.state != "exited" && observed.state != "created") || observed.anchor.Restart.Name != "no" {
			t.Fatal("old container was not joined with restart disabled")
		}
	}
}

func coldDisposition(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var value struct {
		State string `json:"state"`
	}
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	return value.State
}

func (f *ordinaryColdFixture) coldCutState(stage string) bool {
	op := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	disposition := coldDisposition(filepath.Join(f.work, "maintenance", "ordinary", "disposition.json"))
	intentPath := filepath.Join(op, "intent.json")
	journal := filepath.Join(f.work, ".ordinary-fences", f.operation, "fences", f.operation+".json")
	network := filepath.Join(f.work, ".ordinary-fences", f.operation, "fences", f.operation+".network.json")
	participants, _ := filepath.Glob(filepath.Join(op, "participant-*.json"))
	switch stage {
	case "override":
		return disposition == "CLEAN" && !coldExists(intentPath) && !coldExists(filepath.Join(op, "compose-override.yaml"))
	case "intent":
		return disposition == "CLEAN" && !coldExists(intentPath) && coldExists(filepath.Join(op, "compose-override.yaml"))
	case "active":
		return disposition == "CLEAN" && coldExists(intentPath)
	case "successor-active":
		state, err := ordinaryObserveDisposition(filepath.Join(f.work, "maintenance", "ordinary"))
		return err == nil && state.State == "COMMITTED_READY_FOR_RESTART" && state.OperationID == f.previousOperation && coldExists(intentPath)
	case "fence":
		return disposition == "ACTIVE" && coldExists(intentPath) && !coldExists(journal)
	case "participant":
		return disposition == "ACTIVE" && coldExists(journal) && coldExists(network) && len(participants) == 0
	case "rotation":
		return disposition == "ACTIVE" && coldExists(filepath.Join(f.work, "data", "records-authority", "private", "membership-successor.json")) && !coldExists(filepath.Join(f.work, "data", "records-authority", "private", "membership-rotation-receipt.json"))
	case "ready":
		return disposition == "ACTIVE" && coldExists(filepath.Join(op, "restoring.json")) && !coldExists(filepath.Join(op, "ready.json"))
	case "runtime":
		files, _ := filepath.Glob(filepath.Join(op, "runtime-*.json"))
		return disposition == "COMMITTED_READY_FOR_RESTART" && coldExists(filepath.Join(op, "ready.json")) && len(files) == 0
	}
	if disposition != "ACTIVE" || len(participants) == 0 {
		return false
	}
	intentBody, err := os.ReadFile(intentPath)
	var intent ordinaryUpgradeIntent
	if err != nil || json.Unmarshal(intentBody, &intent) != nil || ordinaryValidateIntent(intent) != nil {
		return false
	}
	centerPath := filepath.Join(f.work, "data", "center-config", composeCenterBirthLaunchFile)
	processorPath := filepath.Join(f.work, "data", "center-config", composeProcessorBirthLaunchFile)
	center, e1 := os.ReadFile(centerPath)
	processor, e2 := os.ReadFile(processorPath)
	var cs, ps unix.Stat_t
	if e1 != nil || e2 != nil || unix.Lstat(centerPath, &cs) != nil || unix.Lstat(processorPath, &ps) != nil {
		return false
	}
	for _, stat := range []unix.Stat_t{cs, ps} {
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 10002 || stat.Gid != 10001 || stat.Mode&0o7777 != 0o440 || stat.Nlink != 1 {
			return false
		}
	}
	switch stage {
	case "staged":
		return bytes.Equal(center, intent.Predecessor.CenterLaunch) && bytes.Equal(processor, intent.Predecessor.ProcessorLaunch)
	case "mixed":
		return bytes.Equal(center, intent.Target.CenterLaunch) && bytes.Equal(processor, intent.Predecessor.ProcessorLaunch) && !bytes.Equal(processor, intent.Target.ProcessorLaunch)
	case "restoring":
		return bytes.Equal(center, intent.Target.CenterLaunch) && bytes.Equal(processor, intent.Target.ProcessorLaunch) && !coldExists(filepath.Join(op, "restoring.json"))
	}
	return false
}

func (f *ordinaryColdFixture) coldStagedRevision(t *testing.T, opened bool) string {
	t.Helper()
	var request ordinaryUpgradeRequest
	requestBody, err := os.ReadFile(f.request)
	if err != nil || json.Unmarshal(requestBody, &request) != nil {
		t.Fatal("cannot read cold request")
	}
	intentBody, err := os.ReadFile(filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation, "intent.json"))
	var intent ordinaryUpgradeIntent
	if err != nil || json.Unmarshal(intentBody, &intent) != nil || ordinaryValidateIntent(intent) != nil {
		t.Fatal("cannot read staged intent")
	}
	query := "SELECT revision::text || '|' || opened::text || '|' || encode(center_binding,'hex') || '|' || encode(processor_binding,'hex') FROM record_platform_internal.record_birth_launch_state WHERE singleton"
	row := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "exec", "--user", "postgres", request.DatabaseContainerID, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "houfeng", "-c", query)))
	fields := strings.Split(row, "|")
	revision, parseErr := strconv.ParseInt(fields[0], 10, 64)
	if len(fields) != 4 || parseErr != nil || revision != intent.Predecessor.RegistryRevision+1 || fields[1] != strconv.FormatBool(opened) || fields[2] != hex.EncodeToString(intent.Target.CenterLaunch) || fields[3] != hex.EncodeToString(intent.Target.ProcessorLaunch) {
		t.Fatal("exact committed target stage was not observed")
	}
	return fields[0]
}

// Crash cases pre-create only the real product's empty operation directories
// under the permanent controller lock. The positive case never calls this.
func (f *ordinaryColdFixture) coldPrepareCutDirectories(t *testing.T) {
	t.Helper()
	op := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	fence := filepath.Join(f.work, ".ordinary-fences", f.operation)
	if coldExists(op) || coldExists(fence) {
		t.Fatal("crash fixture operation directories already exist")
	}
	err := ordinaryWithControllerLock(f.ctx, f.work, func() error {
		if err := ordinaryBootstrapGuard(f.work); err != nil {
			return err
		}
		prepare := func() error {
			dirs, err := ordinaryOpenOperationDirectories(f.work, f.operation, true)
			if err != nil {
				return err
			}
			defer dirs.Close()
			fd, err := fenceJournalDir(dirs.fenceConfigPath)
			if err != nil {
				return err
			}
			return unix.Close(fd)
		}
		if f.previousOperation != "" {
			return WithOrdinaryComposeGuard(f.ctx, filepath.Join(f.work, "maintenance", "ordinary"), true, prepare)
		}
		return prepare()
	})
	if err != nil {
		t.Fatalf("product could not prepare crash observation directories: %v", err)
	}
	for _, item := range []struct {
		path     string
		uid, gid uint32
		mode     uint32
	}{
		{filepath.Dir(op), 0, 10001, 0o750}, {op, 0, 10001, 0o750},
		{filepath.Join(op, "backends"), 10002, 10001, 0o700},
		{filepath.Dir(fence), 0, 0, 0o700}, {fence, 0, 0, 0o700},
		{filepath.Join(fence, "fences"), 0, 0, 0o700},
	} {
		var st unix.Stat_t
		if unix.Lstat(item.path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != item.uid || st.Gid != item.gid || st.Mode&0o7777 != item.mode {
			t.Fatal("prepared crash directory has unexpected metadata")
		}
	}
	for _, item := range []struct{ path, only string }{{op, "backends"}, {filepath.Join(op, "backends"), ""}, {fence, "fences"}, {filepath.Join(fence, "fences"), ""}} {
		entries, err := os.ReadDir(item.path)
		if err != nil || (item.only == "" && len(entries) != 0) || (item.only != "" && (len(entries) != 1 || entries[0].Name() != item.only || !entries[0].IsDir())) {
			t.Fatal("prepared crash directories contain unexpected state")
		}
	}
	state, err := ordinaryObserveDisposition(filepath.Join(f.work, "maintenance", "ordinary"))
	if err != nil || (f.previousOperation == "" && state.State != "CLEAN") ||
		(f.previousOperation != "" && (state.State != "COMMITTED_READY_FOR_RESTART" || state.OperationID != f.previousOperation)) {
		t.Fatal("prepared crash guard did not retain its initial disposition")
	}
}

func (f *ordinaryColdFixture) coldRunUpgrade(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	logName := f.operation + "-" + label + "-cli.log"
	logPath := filepath.Join("/root/fixture-evidence", logName)
	output, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	parent, err := os.Open("/root/fixture-evidence")
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Sync(); err != nil {
		_ = parent.Close()
		t.Fatal(err)
	}
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	pidfd := -1
	cmd := exec.Command(f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	cmd.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if pidfd < 0 {
		t.Fatal("upgrade did not receive a clone-time pidfd")
	}
	defer unix.Close(pidfd)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if syncErr := output.Sync(); syncErr != nil {
			t.Fatal(syncErr)
		}
		if err != nil {
			t.Fatalf("ordinary upgrade step %s failed: %v (private %s)", label, err, logName)
		}
	case <-ctx.Done():
		signalErr := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
		if errors.Is(signalErr, unix.ESRCH) {
			signalErr = nil
		}
		joined := false
		select {
		case <-done:
			joined = true
		case <-time.After(5 * time.Second):
		}
		syncErr := output.Sync()
		status, err := json.Marshal(struct {
			Operation    string `json:"operation"`
			Step         string `json:"step"`
			PID          int    `json:"pid"`
			Joined       bool   `json:"joined"`
			Deadline     string `json:"deadline"`
			SignalError  string `json:"signal_error"`
			LogSyncError string `json:"log_sync_error"`
		}{f.operation, label, cmd.Process.Pid, joined, ctx.Err().Error(), coldErrorString(signalErr), coldErrorString(syncErr)})
		if err != nil {
			t.Fatal(err)
		}
		coldPersistEvidence(t, f.operation+"-"+label+"-timeout.json", append(status, '\n'))
		t.Fatalf("ordinary upgrade step %s exceeded its deadline; joined=%t, signal error=%s (private %s)", label, joined, coldErrorString(signalErr), logName)
	}
}

func coldErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (f *ordinaryColdFixture) coldCut(t *testing.T, stage string) {
	f.coldCutBeforeRetry(t, stage, nil)
}

func (f *ordinaryColdFixture) coldCutBeforeRetry(t *testing.T, stage string, beforeRetry func()) {
	t.Helper()
	caseCtx, cancel := context.WithTimeout(f.ctx, 6*time.Minute)
	originalCtx := f.ctx
	f.ctx = caseCtx
	defer func() { f.ctx = originalCtx; cancel() }()
	f.coldPrepareCutDirectories(t)
	op := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation)
	parent := op
	switch stage {
	case "active", "successor-active":
		parent = filepath.Join(f.work, "maintenance", "ordinary")
	case "fence":
		parent = filepath.Join(f.work, ".ordinary-fences", f.operation, "fences")
	case "rotation":
		parent = filepath.Join(f.work, "data", "records-authority", "private")
	case "staged", "mixed":
		parent = filepath.Join(f.work, "data", "center-config")
	}
	baselineTemps := coldTemps(parent)
	group := coldFanotify(t, op, parent)
	pidfd := -1
	cmd := exec.Command(f.admin, "deploy-ordinary-upgrade", "--request-file", f.request)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	cmd.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		_ = unix.Close(group)
		t.Fatal(err)
	}
	if pidfd < 0 {
		_ = unix.Close(group)
		t.Fatal("controller did not receive a clone-time pidfd")
	}
	finished := make(chan struct{})
	go func() { _ = cmd.Wait(); close(finished) }()
	w := &coldPermissionWatcher{stop: make(chan struct{}), done: make(chan struct{}), faults: make(chan error, 1), events: make(chan coldPermissionEvent, 64), decisions: make(chan coldPermissionDecision, 64)}
	go w.run(group, cmd.Process.Pid)
	watchStop := make(chan struct{})
	watchDone := make(chan struct{})
	watchErr := make(chan error, 1)
	var watchOnce sync.Once
	stopWatchdog := func() { watchOnce.Do(func() { close(watchStop) }) }
	go func() {
		defer close(watchDone)
		var cause error
		select {
		case cause = <-w.faults:
		case <-caseCtx.Done():
			cause = fmt.Errorf("crash case deadline: %w", caseCtx.Err())
		case <-w.done:
			select {
			case <-watchStop:
				return
			case cause = <-w.faults:
			default:
				cause = errors.New("fanotify listener stopped unexpectedly")
			}
		case <-watchStop:
			return
		}
		w.stopWatching()
		<-w.done
		if child := int(w.childFD.Load()); child > 0 {
			if err := unix.PidfdSendSignal(child, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				cause = errors.Join(cause, fmt.Errorf("participant pidfd termination: %w", err))
			}
		}
		if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			cause = errors.Join(cause, fmt.Errorf("controller pidfd termination: %w", err))
		}
		fmt.Fprintf(os.Stderr, "ordinary cold crash watchdog released permission events: %v\n", cause)
		select {
		case watchErr <- cause:
		default:
		}
		cancel()
	}()
	defer func() {
		stopWatchdog()
		w.stopWatching()
		<-w.done
		<-watchDone
		if child := int(w.childFD.Load()); child > 0 {
			if err := unix.PidfdSendSignal(child, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				fmt.Fprintf(os.Stderr, "ordinary cold participant cleanup failed: %v\n", err)
			}
			_ = unix.Close(child)
		}
		if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			fmt.Fprintf(os.Stderr, "ordinary cold controller cleanup failed: %v\n", err)
		}
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
		}
		_ = unix.Close(pidfd)
	}()
	allow := func(event coldPermissionEvent) {
		if err := w.reply(caseCtx, event, unix.FAN_ALLOW); err != nil {
			t.Fatalf("permission ALLOW failed: %v", err)
		}
	}
	var selected coldPermissionEvent
	var selectedStat unix.Stat_t
	firstTempOpens := 0
	var stagedRevision string
	for selected.fd == 0 {
		select {
		case event := <-w.events:
			path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", event.fd))
			if err != nil {
				allow(event)
				continue
			}
			event.path = path
			base := filepath.Base(path)
			// The controller rereads this durable child record before its ACK.
			// Pin and verify that exact participant before allowing the read.
			if event.pid == cmd.Process.Pid && filepath.Dir(path) == op && ordinaryParticipantRecordName.MatchString(base) && w.childPID.Load() == 0 {
				body, err := os.ReadFile(path)
				var record ordinaryParticipantRecord
				intent, intentErr := os.ReadFile(filepath.Join(op, "intent.json"))
				if err != nil || intentErr != nil || fenceDecode(body, &record) != nil || record.Version != 1 || record.OperationID != f.operation || record.IntentSHA256 != ordinaryDigestHex(intent) || ordinaryParticipantName(record.Identity) != base || record.Identity.ExecutableSHA256 != f.targetAdminHash || !coldDescendant(record.Identity.PID, cmd.Process.Pid) {
					t.Fatal("invalid participant identity before EXECUTE")
				}
				child, err := unix.PidfdOpen(record.Identity.PID, 0)
				if err != nil {
					t.Fatal("cannot pin exact participant")
				}
				if ordinaryNativeInspect(child, record.Identity) != nil {
					_ = unix.Close(child)
					t.Fatal("participant identity changed before EXECUTE")
				}
				w.childFD.Store(int64(child))
				w.childPID.Store(int64(record.Identity.PID))
			}
			candidate := filepath.Dir(path) == parent && (strings.HasPrefix(base, ".ordinary-launch-") || strings.HasPrefix(base, ".birth-launch-") || strings.HasPrefix(base, ".tmp-") || strings.HasPrefix(base, ".membership.tmp-"))
			if (stage == "intent" || stage == "override") && candidate && f.coldCutState(stage) {
				firstTempOpens++
				if firstTempOpens != 1 || event.pid != cmd.Process.Pid || !ordinaryOperationTemporaryName.MatchString(base) {
					t.Fatal("first eligible operation temporary open is not the requested boundary")
				}
			}
			if !candidate || !f.coldCutState(stage) {
				allow(event)
				continue
			}
			var stat unix.Stat_t
			if unix.Fstat(event.fd, &stat) != nil || stat.Size != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
				if stage == "intent" || stage == "override" {
					t.Fatal("first pre-intent temporary open is not an empty regular file")
				}
				allow(event)
				continue
			}
			if stage == "staged" || stage == "mixed" {
				stagedRevision = f.coldStagedRevision(t, false)
			}
			if stage == "ready" {
				f.coldRestoredNetwork(t)
				f.coldOldContainersStopped(t)
			}
			if stage == "runtime" {
				if _, err := ordinaryVerifySelectedProcesses(caseCtx, f.coldIntent(t)); err != nil {
					t.Fatal("selected target processes were not observable before runtime receipt")
				}
			}
			if stage == "rotation" && f.coldRotationFacts(t) != "2|0" {
				allow(event)
				continue
			}
			selected = event
			selectedStat = stat
		case <-finished:
			coldWrite(t, "/root/fixture-evidence/early-cli.log", output.Bytes(), 0o600)
			t.Fatalf("controller exited before observed %s cut", stage)
		case err := <-watchErr:
			t.Fatalf("crash observation failed: %v", err)
		case <-caseCtx.Done():
			select {
			case err := <-watchErr:
				t.Fatalf("crash observation failed: %v", err)
			default:
				t.Fatalf("crash case deadline before observed %s cut: %v", stage, caseCtx.Err())
			}
		}
	}
	f.coldBoundaryWitness(t, stage, "pre-kill", parent, selected, selectedStat, cmd.Process.Pid, firstTempOpens, stagedRevision)
	childFD := 0
	if selected.pid != cmd.Process.Pid {
		if int64(selected.pid) != w.childPID.Load() || w.childFD.Load() <= 0 {
			t.Fatal("held event is not the recorded participant")
		}
		childFD = int(w.childFD.Load())
	}
	if err := w.killAndDeny(caseCtx, selected, childFD, pidfd); err != nil {
		t.Fatalf("held-event pidfd kill, exit observation, or denial failed: %v", err)
	}
	if err := caseCtx.Err(); err != nil {
		t.Fatalf("crash case expired while observing the held event: %v", err)
	}
	select {
	case <-finished:
	case <-caseCtx.Done():
		t.Fatal("killed controller did not join before case deadline")
	}
	stopWatchdog()
	w.stopWatching()
	<-w.done
	<-watchDone
	var temp unix.Stat_t
	if unix.Lstat(selected.path, &temp) != nil || temp.Dev != selectedStat.Dev || temp.Ino != selectedStat.Ino || temp.Mode&unix.S_IFMT != unix.S_IFREG || temp.Size != 0 || temp.Nlink != 1 || !f.coldCutState(stage) {
		t.Fatalf("%s cut did not leave the independently witnessed pre-publication state and temp", stage)
	}
	newTemps := coldTemps(parent)
	newCount := 0
	for path := range newTemps {
		if !baselineTemps[path] {
			newCount++
			if path != selected.path {
				t.Fatal("unexpected additional new phase temp")
			}
		}
	}
	if newCount != 1 {
		t.Fatal("cut did not leave exactly one newly created phase temp")
	}
	f.coldBoundaryWitness(t, stage, "pre-retry", parent, selected, selectedStat, cmd.Process.Pid, firstTempOpens, stagedRevision)
	if beforeRetry != nil {
		beforeRetry()
		return
	}
	f.coldRunUpgrade(t, caseCtx, stage+"-retry")
	if stage == "intent" || stage == "override" {
		request, err := ordinaryReadRequestFile(f.request)
		expected, expectedErr := ordinaryComposeOverride(request)
		actual, readErr := os.ReadFile(filepath.Join(op, "compose-override.yaml"))
		if err != nil || expectedErr != nil || readErr != nil || !bytes.Equal(actual, expected) {
			t.Fatal("exact retry did not preserve the canonical Compose override")
		}
	}
	if (stage == "staged" || stage == "mixed") && f.coldStagedRevision(t, true) != stagedRevision {
		t.Fatal("stage retry created a second revision")
	}
	if stage == "rotation" && (f.coldRotationFacts(t) != "2|0" || !coldExists(filepath.Join(f.work, "data", "records-authority", "private", "membership-rotation-receipt.json"))) {
		t.Fatal("rotation retry did not preserve the exact committed projection and receipt")
	}
	f.waitHTTP(t)
	if coldExists(selected.path) {
		t.Logf("%s retry accepted the original leftover temp", stage)
	} else {
		t.Logf("%s retry cleaned the original leftover temp", stage)
	}
	t.Logf("observed %s pre-publication cut and exact-operation retry", stage)
}

func TestOrdinaryColdCrashRecovery(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	oldImage, targetImage := os.Getenv("HOUFENG_ORDINARY_COLD_OLD_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_IMAGE")
	if os.Geteuid() != 0 || oldImage == "" || targetImage == "" || oldImage == targetImage {
		t.Fatal("missing isolated root fixture images")
	}
	for _, stage := range []string{"override", "intent", "active", "fence", "participant", "staged", "mixed", "restoring", "ready", "runtime"} {
		if !t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()
			f := newOrdinaryColdFixture(t, ctx, oldImage, targetImage)
			defer f.proxy.Close()
			f.login(t)
			f.coldCut(t, stage)
			f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
		}) {
			t.FailNow()
		}
	}
}

func TestOrdinaryColdForeignOverrideRefused(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	old, target := os.Getenv("HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || old == "" || target == "" || old == target {
		t.Fatal("missing isolated fixture images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, old, target)
	defer f.proxy.Close()
	before := f.coldStackSnapshot(t)
	f.coldCutBeforeRetry(t, "intent", func() {
		path := filepath.Join(f.work, "maintenance", "ordinary-operations", f.operation, "compose-override.yaml")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		coldWrite(t, path, append(body, []byte("# foreign bytes\n")...), 0o600)
	})
	f.coldAssertPreflightRefusal(t, "compose-recipe", before)
}

func (f *ordinaryColdFixture) coldRotationFacts(t *testing.T) string {
	t.Helper()
	var request ordinaryUpgradeRequest
	body, err := os.ReadFile(f.request)
	if err != nil || json.Unmarshal(body, &request) != nil {
		t.Fatal("cannot read released fixture request")
	}
	query := "SELECT count(*) FILTER (WHERE instance_id IN ('compose-center','compose-processor') AND deployment_epoch=2)::text || '|' || count(*) FILTER (WHERE deployment_epoch=1)::text FROM public.deployment_membership"
	return strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "exec", "--user", "postgres", request.DatabaseContainerID, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "houfeng", "-c", query)))
}

func TestOrdinaryColdReleasedPredecessor(t *testing.T) {
	if os.Getenv("HOUFENG_ORDINARY_COLD_E2E") != "1" {
		t.Skip("requires the isolated ordinary cold Docker fixture")
	}
	released, target := os.Getenv("HOUFENG_ORDINARY_COLD_RELEASE_IMAGE"), os.Getenv("HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE")
	if os.Geteuid() != 0 || released == "" || target == "" || released == target {
		t.Fatal("missing released and final target images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	f := newOrdinaryColdFixture(t, ctx, released, target)
	defer f.proxy.Close()
	ordinaryColdAssertC80Admission(t, f)
	root := filepath.Join(f.work, "data", "records-authority")
	before, err := recordauthority.LoadComposeState(root)
	if err != nil || before.MembershipVersion != 1 || before.MembershipRotation != nil || recordauthority.VerifyActivationReceipt(root, before) != nil {
		t.Fatal("released image did not produce authentic valid V1 signed state")
	}
	if f.coldRotationFacts(t) != "0|1" {
		t.Fatal("released database is not the expected active V1 projection")
	}
	witness := make(map[string][32]byte)
	for _, name := range []string{"authority-key", "database-secret", "activation-ledger.jsonl", "activation-receipt.json"} {
		body, err := os.ReadFile(filepath.Join(root, "private", name))
		if err != nil {
			t.Fatal("released authority component missing")
		}
		witness[name] = sha256.Sum256(body)
	}
	f.login(t)
	subject := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/vps", map[string]any{"display_name": "released fixture source", "lifecycle_status": "active"}, nil))
	content := "# Released predecessor content\n" + coldRandom(t, 16) + "\n"
	draft := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/record-drafts", map[string]any{"payload": f.payload(coldString(t, subject, "vps_id"), content, []string{})}, nil))
	created := coldJSON(t, f.requestHTTP(t, 201, "POST", "/api/records", map[string]any{"draft_id": coldString(t, draft, "draft_id"), "draft_etag": coldString(t, draft, "etag")}, map[string]string{"Idempotency-Key": "ordinary-released-old"}))
	recordID := coldString(t, created, "record_id")
	f.coldCut(t, "rotation")
	after, err := recordauthority.LoadComposeState(root)
	if err != nil || after.MembershipVersion != 2 || after.MembershipRotation == nil || after.ActivationCommand != before.ActivationCommand || recordauthority.VerifyActivationReceipt(root, after) != nil || recordauthority.VerifyMembershipRotationReceipt(root, after) != nil {
		t.Fatal("released V1 signed chain was not preserved and upgraded")
	}
	for name, digest := range witness {
		body, err := os.ReadFile(filepath.Join(root, "private", name))
		if err != nil || sha256.Sum256(body) != digest {
			t.Fatal("released signed predecessor bytes changed")
		}
	}
	f.requestHTTP(t, 200, "GET", "/api/auth/me", nil, nil)
	got := coldJSON(t, f.requestHTTP(t, 200, "GET", "/api/records/"+recordID, nil, nil))
	current, ok := got["current"].(map[string]any)
	if !ok || coldString(t, current, "body_markdown") != content {
		t.Fatal("released old record content changed")
	}
}
