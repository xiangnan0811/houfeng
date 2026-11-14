//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

var errComposeOfflineFence = errors.New("compose offline fence rejected or operation uncertain")
var fenceRID = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)
var fenceProject = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var fenceID = regexp.MustCompile(`^[a-f0-9]{64}$`)

const fenceOutputLimit = 8 << 20

type fenceLimitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *fenceLimitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := fenceOutputLimit - b.Len()
	if remaining < n {
		b.overflow = true
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		return n, nil
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func fenceDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...)
	// Do not inherit Docker context, config, credentials or other client settings.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	var out, stderr fenceLimitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil || out.overflow || stderr.overflow {
		return nil, errComposeOfflineFence
	}
	return out.Bytes(), nil
}

func fenceDockerBinary() error {
	var s unix.Stat_t
	if unix.Lstat("/usr/bin/docker", &s) != nil || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Uid != 0 || s.Mode&0o022 != 0 {
		return errComposeOfflineFence
	}
	return nil
}

type fenceMount struct {
	Type   string `json:"Type"`
	Name   string `json:"Name"`
	Source string `json:"Source"`
}
type fenceNetwork struct {
	ID      string   `json:"NetworkID"`
	Aliases []string `json:"Aliases"`
	IP      string   `json:"IPAddress"`
}
type fenceRestart struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount"`
}
type fenceContainer struct {
	ID               string               `json:"id"`
	Image            string               `json:"image"`
	Name             string               `json:"name"`
	Service          string               `json:"service"`
	Project          string               `json:"project"`
	WorkingDirectory string               `json:"working_directory"`
	Restart          fenceRestart         `json:"restart"`
	Networks         []fenceNetworkAnchor `json:"networks"`
	Mounts           []fenceMount         `json:"mounts"`
}
type fenceNetworkAnchor struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
}
type fenceObservation struct {
	anchor      fenceContainer
	state       string
	networkMode string
	pidMode     string
	published   bool
	networks    map[string]fenceNetwork
	scheduler   bool
}
type fenceJournal struct {
	Version    int                        `json:"version"`
	Request    ComposeOfflineFenceRequest `json:"request"`
	Containers []fenceContainer           `json:"containers"`
}
type fenceNetworkReceipt struct {
	Version    int    `json:"version"`
	RecoveryID string `json:"recovery_id"`
	NetworkID  string `json:"network_id"`
}

// The template emits only fields needed for the inventory. In particular it never
// requests Config.Env, command arguments, container logs or credential material.
const fenceInspectTemplate = `{{json .Id}}	{{json .Image}}	{{json .Name}}	{{json .Config.Labels}}	{{json .HostConfig.RestartPolicy}}	{{json .Mounts}}	{{json .NetworkSettings.Networks}}	{{json .State.Status}}	{{json .HostConfig.NetworkMode}}	{{json .HostConfig.PidMode}}	{{json .HostConfig.PortBindings}}	{{json .NetworkSettings.Ports}}`

func fenceInspect(ctx context.Context, id string) (fenceObservation, error) {
	var o fenceObservation
	data, err := fenceDocker(ctx, "inspect", "--format", fenceInspectTemplate, id)
	if err != nil {
		return o, err
	}
	fields := strings.Split(strings.TrimSpace(string(data)), "\t")
	if len(fields) != 12 {
		return o, errComposeOfflineFence
	}
	var labels map[string]string
	var mounts []fenceMount
	var bindings, ports map[string]json.RawMessage
	targets := []any{&o.anchor.ID, &o.anchor.Image, &o.anchor.Name, &labels, &o.anchor.Restart, &mounts, &o.networks, &o.state, &o.networkMode, &o.pidMode, &bindings, &ports}
	for i := range targets {
		if json.Unmarshal([]byte(fields[i]), targets[i]) != nil {
			return fenceObservation{}, errComposeOfflineFence
		}
	}
	if o.anchor.ID != id || !fenceID.MatchString(id) {
		return fenceObservation{}, errComposeOfflineFence
	}
	o.anchor.Project = labels["com.docker.compose.project"]
	o.anchor.Service = labels["com.docker.compose.service"]
	o.anchor.WorkingDirectory = labels["com.docker.compose.project.working_dir"]
	for k := range labels {
		if strings.HasPrefix(k, "com.docker.swarm.") || strings.HasPrefix(k, "com.docker.stack.") || strings.HasPrefix(k, "io.kubernetes.") || strings.HasPrefix(k, "com.hashicorp.nomad.") {
			o.scheduler = true
		}
	}
	for _, m := range mounts {
		if m.Type != "bind" && m.Type != "volume" && m.Type != "tmpfs" {
			return fenceObservation{}, errComposeOfflineFence
		}
		if m.Type == "bind" && (!filepath.IsAbs(m.Source) || filepath.Clean(m.Source) != m.Source) {
			return fenceObservation{}, errComposeOfflineFence
		}
		if m.Type == "volume" && m.Name == "" {
			return fenceObservation{}, errComposeOfflineFence
		}
		o.anchor.Mounts = append(o.anchor.Mounts, m)
	}
	sort.Slice(o.anchor.Mounts, func(i, j int) bool {
		a, b := o.anchor.Mounts[i], o.anchor.Mounts[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Source < b.Source
	})
	for _, n := range o.networks {
		// Docker retains the requested network on a never-started container
		// without an attached endpoint. Its mounts still participate in inventory.
		if n.ID == "" && n.IP == "" && o.state == "created" {
			continue
		}
		if !fenceID.MatchString(n.ID) {
			return fenceObservation{}, errComposeOfflineFence
		}
		aliases := append([]string(nil), n.Aliases...)
		sort.Strings(aliases)
		o.anchor.Networks = append(o.anchor.Networks, fenceNetworkAnchor{ID: n.ID, Aliases: aliases})
	}
	sort.Slice(o.anchor.Networks, func(i, j int) bool { return o.anchor.Networks[i].ID < o.anchor.Networks[j].ID })
	for _, v := range bindings {
		if string(v) != "null" && string(v) != "[]" {
			o.published = true
		}
	}
	for _, v := range ports {
		if string(v) != "null" && string(v) != "[]" {
			o.published = true
		}
	}
	return o, nil
}

func fenceListContainers(ctx context.Context) ([]string, error) {
	b, err := fenceDocker(ctx, "ps", "-a", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, id := range strings.Fields(string(b)) {
		if !fenceID.MatchString(id) {
			return nil, errComposeOfflineFence
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return nil, errComposeOfflineFence
		}
	}
	return ids, nil
}

func fenceOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator)) || a == "/" || b == "/"
}
func fenceMountsOverlap(a, b fenceMount) (bool, error) {
	if a.Type == "volume" && b.Type == "volume" && a.Name == b.Name {
		return true, nil
	}
	if (a.Type != "bind" && a.Type != "volume") || (b.Type != "bind" && b.Type != "volume") {
		return false, nil
	}
	if !filepath.IsAbs(a.Source) || !filepath.IsAbs(b.Source) {
		return false, errComposeOfflineFence
	}
	aa, e1 := filepath.EvalSymlinks(a.Source)
	bb, e2 := filepath.EvalSymlinks(b.Source)
	if e1 != nil || e2 != nil {
		return false, errComposeOfflineFence
	}
	return fenceOverlap(aa, bb), nil
}

func fenceInventory(ctx context.Context, r ComposeOfflineFenceRequest) ([]fenceObservation, error) {
	ids, err := fenceListContainers(ctx)
	if err != nil {
		return nil, err
	}
	var project, foreign []fenceObservation
	for _, id := range ids {
		o, e := fenceInspect(ctx, id)
		if e != nil {
			return nil, e
		}
		if o.anchor.Project == r.Project {
			project = append(project, o)
		} else {
			foreign = append(foreign, o)
		}
	}
	dbs := 0
	for _, o := range project {
		if o.anchor.WorkingDirectory != r.WorkingDirectory || o.anchor.Service == "" || o.scheduler || o.state == "paused" || o.state == "dead" || o.state == "removing" || o.state == "restarting" || o.networkMode == "host" || strings.HasPrefix(o.networkMode, "container:") || o.pidMode == "host" || strings.HasPrefix(o.pidMode, "container:") {
			return nil, errComposeOfflineFence
		}
		if o.anchor.Service == "db" {
			dbs++
			if o.anchor.ID != r.DatabaseContainerID || o.published || o.state != "running" {
				return nil, errComposeOfflineFence
			}
		}
	}
	if dbs != 1 {
		return nil, errComposeOfflineFence
	}
	// Conservatively protect every project mount, including authority and secret
	// roots, rather than guessing which service owns a sensitive bind.
	for _, p := range project {
		for _, m := range p.anchor.Mounts {
			if m.Type == "bind" || m.Type == "volume" {
				control, e1 := filepath.EvalSymlinks(filepath.Dir(r.ControlConfigPath))
				source, e2 := filepath.EvalSymlinks(m.Source)
				if e1 != nil || e2 != nil || fenceOverlap(control, source) {
					return nil, errComposeOfflineFence
				}
			}
			for _, f := range foreign {
				for _, n := range f.anchor.Mounts {
					overlap, e := fenceMountsOverlap(m, n)
					if e != nil || overlap {
						return nil, errComposeOfflineFence
					}
				}
			}
		}
	}
	return project, nil
}

func fenceRequestValid(r ComposeOfflineFenceRequest) bool {
	if !fenceRID.MatchString(r.RecoveryID) || !fenceProject.MatchString(r.Project) || !fenceID.MatchString(r.DatabaseContainerID) || !filepath.IsAbs(r.WorkingDirectory) || filepath.Clean(r.WorkingDirectory) != r.WorkingDirectory || !filepath.IsAbs(r.ControlConfigPath) || filepath.Clean(r.ControlConfigPath) != r.ControlConfigPath {
		return false
	}
	work, e := filepath.EvalSymlinks(r.WorkingDirectory)
	if e != nil || work != r.WorkingDirectory {
		return false
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(r.ControlConfigPath))
	if e != nil {
		return false
	}
	data := filepath.Join(work, "data")
	if resolved, e := filepath.EvalSymlinks(data); e == nil {
		data = resolved
	} else if !errors.Is(e, os.ErrNotExist) {
		return false
	}
	return !fenceOverlap(parent, data)
}

func fenceSameAnchors(a, b fenceContainer) bool {
	a.Restart = fenceRestart{}
	b.Restart = fenceRestart{}
	a.Networks = nil
	b.Networks = nil
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func fenceAllowedNetworks(original fenceContainer, observed fenceObservation, maintenanceID string, database bool) bool {
	if !database && len(original.Networks) != len(observed.anchor.Networks) {
		return false
	}
	for _, n := range observed.anchor.Networks {
		if database && n.ID == maintenanceID && maintenanceID != "" {
			continue
		}
		found := false
		for _, old := range original.Networks {
			if old.ID == n.ID {
				a, _ := json.Marshal(old.Aliases)
				b, _ := json.Marshal(n.Aliases)
				if bytes.Equal(a, b) {
					found = true
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// FenceComposeSource stops and joins the project containers, isolates the DB,
// and calls fn while the same exclusive maintenance guard remains held. The
// caller must keep external restart mechanisms and host writers stopped through
// the ceremony. Neither success nor failure restores any resource.
func FenceComposeSource(ctx context.Context, r ComposeOfflineFenceRequest, fn func(ComposeOfflineFence) error) error {
	var result ComposeOfflineFence
	if ctx == nil || fn == nil || !fenceRequestValid(r) {
		return errComposeOfflineFence
	}
	cfg, err := LoadSourceMaintenanceConfig(r.ControlConfigPath)
	if err != nil {
		return errComposeOfflineFence
	}
	var callbackErr error
	err = WithMaintenanceGuard(ctx, cfg.GuardDirectory, cfg.GuardIdentity, true, func() error {
		now, e := LoadSourceMaintenanceConfig(r.ControlConfigPath)
		if e != nil || now != cfg || !fenceRequestValid(r) {
			return errComposeOfflineFence
		}
		if e = fenceDockerBinary(); e != nil {
			return e
		}
		swarm, e := fenceDocker(ctx, "info", "--format", "{{.Swarm.LocalNodeState}}")
		if e != nil || strings.TrimSpace(string(swarm)) != "inactive" {
			return errComposeOfflineFence
		}
		if e = fencePerform(ctx, r, &result); e != nil {
			return e
		}
		now, e = LoadSourceMaintenanceConfig(r.ControlConfigPath)
		if e != nil || now != cfg || ctx.Err() != nil {
			return errComposeOfflineFence
		}
		callbackErr = fn(result)
		return callbackErr
	})
	if callbackErr != nil {
		return callbackErr
	}
	if err != nil {
		return errComposeOfflineFence
	}
	return nil
}

func fencePerform(ctx context.Context, r ComposeOfflineFenceRequest, result *ComposeOfflineFence) error {
	dir, err := fenceJournalDir(r.ControlConfigPath)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	project, err := fenceInventory(ctx, r)
	if err != nil {
		return err
	}
	journal := fenceJournal{Version: 1, Request: r}
	for _, o := range project {
		journal.Containers = append(journal.Containers, o.anchor)
	}
	stored, exists, err := fenceReadJournal(dir, r)
	if err != nil {
		return err
	}
	if !exists {
		if err = fencePublish(dir, r.RecoveryID+".json", journal); err != nil {
			return err
		}
		stored = journal
	}
	receipt, hasReceipt, err := fenceReadReceipt(dir, r.RecoveryID)
	if err != nil {
		return err
	}
	// An observed publication may have lost its previous directory sync at a
	// crash cutpoint; make the exact names durable again before Docker effects.
	if unix.Fsync(dir) != nil || !fenceJournalDirMatches(r.ControlConfigPath, dir) {
		return errComposeOfflineFence
	}
	maintenanceID := ""
	if hasReceipt {
		maintenanceID = receipt.NetworkID
	}
	if len(stored.Containers) != len(project) {
		return errComposeOfflineFence
	}
	for i, o := range project {
		if !fenceSameAnchors(stored.Containers[i], o.anchor) || !fenceAllowedNetworks(stored.Containers[i], o, maintenanceID, o.anchor.ID == r.DatabaseContainerID) {
			return errComposeOfflineFence
		}
	}
	// Reject a conflicting or shared maintenance network before stopping anything.
	network, networkExists, err := fenceFindNetwork(ctx, r)
	if err != nil {
		return err
	}
	if hasReceipt && (!networkExists || receipt.NetworkID != network.ID) {
		return errComposeOfflineFence
	}
	if networkExists && fenceValidateNetworkMembers(network, r.DatabaseContainerID) != nil {
		return errComposeOfflineFence
	}
	// No mutation is retried internally. Explicit replay always re-observes.
	for _, o := range project {
		if o.anchor.Restart.Name != "no" {
			if _, err = fenceDocker(ctx, "update", "--restart=no", o.anchor.ID); err != nil {
				return err
			}
		}
	}
	for _, o := range project {
		if o.anchor.ID != r.DatabaseContainerID && o.state == "running" {
			if _, err = fenceDocker(ctx, "stop", o.anchor.ID); err != nil {
				return err
			}
		}
	}
	// A stopped inspection after docker stop is the join; created/exited are also stopped.
	project, err = fenceInventory(ctx, r)
	if err != nil {
		return err
	}
	if len(project) != len(stored.Containers) {
		return errComposeOfflineFence
	}
	for i, o := range project {
		if !fenceSameAnchors(stored.Containers[i], o.anchor) || !fenceAllowedNetworks(stored.Containers[i], o, maintenanceID, o.anchor.ID == r.DatabaseContainerID) || o.anchor.Restart.Name != "no" || (o.anchor.ID != r.DatabaseContainerID && o.state != "exited" && o.state != "created") {
			return errComposeOfflineFence
		}
	}
	if !fenceJournalDirMatches(r.ControlConfigPath, dir) {
		return errComposeOfflineFence
	}
	network, networkExists, err = fenceFindNetwork(ctx, r)
	if err != nil {
		return err
	}
	if !networkExists {
		if hasReceipt {
			return errComposeOfflineFence
		}
		if _, err = fenceDocker(ctx, "network", "create", "--driver", "bridge", "--label", "houfeng.source.recovery_id="+r.RecoveryID, "--label", "com.docker.compose.project="+r.Project, "houfeng-source-"+r.RecoveryID); err != nil {
			return err
		}
		network, networkExists, err = fenceFindNetwork(ctx, r)
		if err != nil || !networkExists {
			return errComposeOfflineFence
		}
	}
	if hasReceipt && receipt.NetworkID != network.ID {
		return errComposeOfflineFence
	}
	if !hasReceipt {
		receipt = fenceNetworkReceipt{Version: 1, RecoveryID: r.RecoveryID, NetworkID: network.ID}
		if err = fencePublish(dir, r.RecoveryID+".network.json", receipt); err != nil {
			return err
		}
	}
	if err = fenceValidateNetworkMembers(network, r.DatabaseContainerID); err != nil {
		return err
	}
	db, err := fenceInspect(ctx, r.DatabaseContainerID)
	if err != nil {
		return err
	}
	var originalDB fenceContainer
	for _, original := range stored.Containers {
		if original.ID == r.DatabaseContainerID {
			originalDB = original
			break
		}
	}
	if originalDB.ID == "" || !fenceSameAnchors(originalDB, db.anchor) || !fenceAllowedNetworks(originalDB, db, network.ID, true) {
		return errComposeOfflineFence
	}
	if _, ok := fenceDBNetwork(db, network.ID); !ok {
		if _, err = fenceDocker(ctx, "network", "connect", network.ID, r.DatabaseContainerID); err != nil {
			return err
		}
	}
	db, err = fenceInspect(ctx, r.DatabaseContainerID)
	if err != nil {
		return err
	}
	if _, ok := fenceDBNetwork(db, network.ID); !ok || !fenceSameAnchors(originalDB, db.anchor) || !fenceAllowedNetworks(originalDB, db, network.ID, true) {
		return errComposeOfflineFence
	}
	for _, n := range db.anchor.Networks {
		if n.ID != network.ID {
			if _, err = fenceDocker(ctx, "network", "disconnect", n.ID, r.DatabaseContainerID); err != nil {
				return err
			}
		}
	}
	if !fenceJournalDirMatches(r.ControlConfigPath, dir) {
		return errComposeOfflineFence
	}
	project, err = fenceInventory(ctx, r)
	if err != nil || len(project) != len(stored.Containers) {
		return errComposeOfflineFence
	}
	var address string
	for i, o := range project {
		if !fenceSameAnchors(stored.Containers[i], o.anchor) || !fenceAllowedNetworks(stored.Containers[i], o, network.ID, o.anchor.ID == r.DatabaseContainerID) || o.anchor.Restart.Name != "no" {
			return errComposeOfflineFence
		}
		if o.anchor.ID == r.DatabaseContainerID {
			if len(o.anchor.Networks) != 1 {
				return errComposeOfflineFence
			}
			n, ok := fenceDBNetwork(o, network.ID)
			if !ok || net.ParseIP(n.IP) == nil {
				return errComposeOfflineFence
			}
			address = n.IP
		} else if o.state != "exited" && o.state != "created" {
			return errComposeOfflineFence
		}
	}
	network, networkExists, err = fenceFindNetwork(ctx, r)
	if err != nil || !networkExists || network.ID != receipt.NetworkID || fenceValidateNetworkMembers(network, r.DatabaseContainerID) != nil || len(network.Containers) != 1 {
		return errComposeOfflineFence
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return errComposeOfflineFence
	}
	*result = ComposeOfflineFence{RecoveryID: r.RecoveryID, DatabaseContainerID: r.DatabaseContainerID, MaintenanceNetworkID: network.ID, DatabaseAddress: address, InventoryDigest: sha256.Sum256(data)}
	return nil
}

func fenceDBNetwork(o fenceObservation, id string) (fenceNetwork, bool) {
	for _, n := range o.networks {
		if n.ID == id {
			return n, true
		}
	}
	return fenceNetwork{}, false
}

type fenceDockerNetwork struct {
	ID         string                     `json:"Id"`
	Name       string                     `json:"Name"`
	Driver     string                     `json:"Driver"`
	Internal   bool                       `json:"Internal"`
	Labels     map[string]string          `json:"Labels"`
	Containers map[string]json.RawMessage `json:"Containers"`
	Scope      string                     `json:"Scope"`
	Ingress    bool                       `json:"Ingress"`
}

func fenceFindNetwork(ctx context.Context, r ComposeOfflineFenceRequest) (fenceDockerNetwork, bool, error) {
	var n fenceDockerNetwork
	name := "houfeng-source-" + r.RecoveryID
	b, err := fenceDocker(ctx, "network", "ls", "--no-trunc", "--filter", "name=^"+name+"$", "--format", "{{.ID}}")
	if err != nil {
		return n, false, err
	}
	ids := strings.Fields(string(b))
	if len(ids) == 0 {
		return n, false, nil
	}
	if len(ids) != 1 || !fenceID.MatchString(ids[0]) {
		return n, false, errComposeOfflineFence
	}
	b, err = fenceDocker(ctx, "network", "inspect", "--format", "{{json .Id}}\t{{json .Name}}\t{{json .Driver}}\t{{json .Internal}}\t{{json .Labels}}\t{{json .Containers}}\t{{json .Scope}}\t{{json .Ingress}}", ids[0])
	if err != nil {
		return n, false, err
	}
	fields := strings.Split(strings.TrimSpace(string(b)), "\t")
	if len(fields) != 8 {
		return n, false, errComposeOfflineFence
	}
	values := []any{&n.ID, &n.Name, &n.Driver, &n.Internal, &n.Labels, &n.Containers, &n.Scope, &n.Ingress}
	for i := range values {
		if json.Unmarshal([]byte(fields[i]), values[i]) != nil {
			return n, false, errComposeOfflineFence
		}
	}
	if n.ID != ids[0] || n.Name != name || n.Driver != "bridge" || n.Internal || n.Scope != "local" || n.Ingress || n.Labels["houfeng.source.recovery_id"] != r.RecoveryID || n.Labels["com.docker.compose.project"] != r.Project {
		return n, false, errComposeOfflineFence
	}
	return n, true, nil
}
func fenceValidateNetworkMembers(n fenceDockerNetwork, db string) error {
	for id := range n.Containers {
		if id != db {
			return errComposeOfflineFence
		}
	}
	return nil
}

// Journal helpers use a private directory anchored under the already verified
// config parent. Publication is immutable and durable before Docker effects.
func fenceJournalDir(config string) (int, error) {
	parent, err := sourceConfigParent(config)
	if err != nil {
		return -1, errComposeOfflineFence
	}
	defer unix.Close(parent)
	if !privateSourceConfigParent(parent) {
		return -1, errComposeOfflineFence
	}
	if err = unix.Mkdirat(parent, "fences", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, errComposeOfflineFence
	}
	if unix.Fsync(parent) != nil {
		return -1, errComposeOfflineFence
	}
	fd, err := unix.Openat2(parent, "fences", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return -1, errComposeOfflineFence
	}
	var s unix.Stat_t
	if unix.Fstat(fd, &s) != nil || s.Mode&0o7777 != 0o700 || s.Uid != uint32(os.Geteuid()) || s.Nlink == 0 {
		unix.Close(fd)
		return -1, errComposeOfflineFence
	}
	return fd, nil
}
func fenceJournalDirMatches(config string, dir int) bool {
	parent, err := sourceConfigParent(config)
	if err != nil {
		return false
	}
	defer unix.Close(parent)
	if !privateSourceConfigParent(parent) {
		return false
	}
	var opened, named unix.Stat_t
	return unix.Fstat(dir, &opened) == nil && unix.Fstatat(parent, "fences", &named, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		opened.Dev == named.Dev && opened.Ino == named.Ino && opened.Mode&unix.S_IFMT == unix.S_IFDIR &&
		opened.Mode&0o7777 == 0o700 && opened.Uid == uint32(os.Geteuid()) && opened.Nlink != 0
}

func fenceReadFile(dir int, name string) ([]byte, bool, error) {
	return fenceReadFileLimit(dir, name, fenceOutputLimit)
}

func fenceReadFileLimit(dir int, name string, limit int64) ([]byte, bool, error) {
	fd, err := unix.Openat2(dir, name, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK), Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errComposeOfflineFence
	}
	f := os.NewFile(uintptr(fd), "fence journal")
	defer f.Close()
	var a, b, c unix.Stat_t
	if unix.Fstat(fd, &a) != nil || a.Mode&unix.S_IFMT != unix.S_IFREG || a.Mode&0o7777 != 0o600 || a.Uid != uint32(os.Geteuid()) || (a.Nlink != 1 && a.Nlink != 2) || a.Size <= 0 || a.Size > limit {
		return nil, false, errComposeOfflineFence
	}
	if a.Nlink == 2 {
		// A crash after link but before unlink may leave exactly our private
		// publication name. Remove only an exact inode-matched temporary link.
		scan, e := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if e != nil {
			return nil, false, errComposeOfflineFence
		}
		directory := os.NewFile(uintptr(scan), "fence directory")
		names, e := directory.Readdirnames(-1)
		directory.Close()
		if e != nil {
			return nil, false, errComposeOfflineFence
		}
		match := ""
		for _, candidate := range names {
			if !regexp.MustCompile(`^\.tmp-[0-9a-f]{32}$`).MatchString(candidate) {
				continue
			}
			var s unix.Stat_t
			if unix.Fstatat(dir, candidate, &s, unix.AT_SYMLINK_NOFOLLOW) != nil {
				return nil, false, errComposeOfflineFence
			}
			if s.Dev == a.Dev && s.Ino == a.Ino {
				if match != "" {
					return nil, false, errComposeOfflineFence
				}
				match = candidate
			}
		}
		if match == "" || unix.Unlinkat(dir, match, 0) != nil || unix.Fsync(dir) != nil || unix.Fstat(fd, &a) != nil || a.Nlink != 1 {
			return nil, false, errComposeOfflineFence
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) != int(a.Size) || unix.Fstat(fd, &b) != nil || unix.Fstatat(dir, name, &c, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(a, b) || !sameSourceFile(a, c) {
		return nil, false, errComposeOfflineFence
	}
	return data, true, nil
}
func fenceDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil {
		return errComposeOfflineFence
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return errComposeOfflineFence
	}
	canonical, err := json.Marshal(v)
	if err != nil || !bytes.Equal(canonical, data) {
		return errComposeOfflineFence
	}
	return nil
}
func fenceReadJournal(dir int, r ComposeOfflineFenceRequest) (fenceJournal, bool, error) {
	var result fenceJournal
	// Reject another RID for the same project or DB; do not silently take over.
	copyFD, err := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return result, false, errComposeOfflineFence
	}
	file := os.NewFile(uintptr(copyFD), "fence directory")
	names, err := file.Readdirnames(-1)
	file.Close()
	if err != nil {
		return result, false, errComposeOfflineFence
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".network.json") {
			continue
		}
		data, ok, e := fenceReadFile(dir, name)
		if e != nil || !ok {
			return result, false, errComposeOfflineFence
		}
		var j fenceJournal
		if fenceDecode(data, &j) != nil || j.Version != 1 || name != j.Request.RecoveryID+".json" || !fenceRID.MatchString(j.Request.RecoveryID) {
			return result, false, errComposeOfflineFence
		}
		if j.Request.RecoveryID == r.RecoveryID {
			if j.Request != r {
				return result, false, errComposeOfflineFence
			}
			result = j
		} else if j.Request.Project == r.Project || j.Request.DatabaseContainerID == r.DatabaseContainerID {
			return result, false, errComposeOfflineFence
		}
	}
	return result, result.Version == 1, nil
}
func fenceReadReceipt(dir int, rid string) (fenceNetworkReceipt, bool, error) {
	var r fenceNetworkReceipt
	b, ok, err := fenceReadFile(dir, rid+".network.json")
	if err != nil || !ok {
		return r, ok, err
	}
	if fenceDecode(b, &r) != nil || r.Version != 1 || r.RecoveryID != rid || !fenceID.MatchString(r.NetworkID) {
		return r, false, errComposeOfflineFence
	}
	return r, true, nil
}
func fencePublish(dir int, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil || len(data) > fenceOutputLimit {
		return errComposeOfflineFence
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return errComposeOfflineFence
	}
	temp := ".tmp-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return errComposeOfflineFence
	}
	f := os.NewFile(uintptr(fd), "fence temporary")
	defer func() { f.Close(); unix.Unlinkat(dir, temp, 0) }()
	if n, e := f.Write(data); e != nil || n != len(data) || f.Sync() != nil {
		return errComposeOfflineFence
	}
	if unix.Linkat(dir, temp, dir, name, 0) != nil {
		return errComposeOfflineFence
	}
	if unix.Unlinkat(dir, temp, 0) != nil || unix.Fsync(dir) != nil {
		return errComposeOfflineFence
	}
	return nil
}
