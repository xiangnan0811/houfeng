//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func sameTemplate(actual, expected map[string]any) bool {
	a, err := canonicalControl(actual)
	if err != nil {
		return false
	}
	b, err := canonicalControl(expected)
	return err == nil && bytes.Equal(a, b)
}

func noLog() map[string]any { return map[string]any{"Type": "none", "Config": map[string]any{}} }
func bind(source, target string, readonly bool) map[string]any {
	return map[string]any{"Type": "bind", "Source": source, "Target": target, "ReadOnly": readonly}
}

// Every field is reproduced from the fixed caller templates. Variable image
// identity and postgres uid/gid must be independently authenticated before
// invoking these constructors; they are not accepted from the create request.
func dnsProbeTemplate() map[string]any {
	return map[string]any{
		"Image": postgresImageID, "Entrypoint": []any{"/opt/g2/dns-probe"}, "Cmd": []any{}, "User": "10002:10002",
		"Env": []any{"PATH=/usr/bin:/bin", "LANG=C"}, "StopSignal": "SIGTERM",
		"HostConfig": map[string]any{"NetworkMode": "g2-dns-preflight", "Dns": []any{"127.0.0.1"},
			"ReadonlyRootfs": true, "Privileged": false, "CapDrop": []any{"ALL"}, "SecurityOpt": []any{"no-new-privileges"},
			"PidsLimit": json.Number("32"), "RestartPolicy": map[string]any{"Name": "no"}, "LogConfig": noLog(),
			"Mounts": []any{bind("/opt/g2/dns-probe", "/opt/g2/dns-probe", true), bind("/run/g2/dns-preflight", "/var/lib/postgresql/data", false)}},
		"NetworkingConfig": map[string]any{"EndpointsConfig": map[string]any{"g2-dns-preflight": map[string]any{"IPAMConfig": map[string]any{"IPv4Address": "172.31.123.2"}}}},
	}
}

func containerBPFSelftestTemplate() map[string]any {
	return map[string]any{
		"Image":      postgresImageID,
		"Entrypoint": []any{"/opt/g2/g2-container-bpf-probe"},
		"Cmd":        []any{"selftest", "container-raw-denial"},
		"User":       "10002:10002",
		"HostConfig": map[string]any{
			"NetworkMode": "none", "ReadonlyRootfs": true, "Privileged": false,
			"CapDrop": []any{"ALL"}, "SecurityOpt": []any{"no-new-privileges"},
			"PidsLimit": json.Number("32"), "RestartPolicy": map[string]any{"Name": "no"}, "LogConfig": noLog(),
			"Mounts": []any{bind("/opt/g2/g2-container-bpf-probe", "/opt/g2/g2-container-bpf-probe", true), bind("/run/g2/container-bpf.fifo", "/run/g2/container-bpf.fifo", true)},
		},
	}
}

func browserTemplate(image string) (map[string]any, error) {
	if len(image) != 71 || image[:7] != "sha256:" {
		return nil, errGuardProtocol
	}
	if _, ok := digestString(image[7:]); !ok {
		return nil, errGuardProtocol
	}
	return map[string]any{
		"Image": image, "Entrypoint": []any{"/opt/g2/browser/chrome-headless-shell"},
		"Cmd":  []any{"--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--disable-background-networking", "--no-first-run", "--no-default-browser-check", "--disable-breakpad", "--disable-crash-reporter", "--user-data-dir=/tmp/g2-browser-profile", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9222", "about:blank"},
		"User": "10002:10002", "Env": []any{"LD_LIBRARY_PATH=/opt/g2/browser:/opt/g2/runtime/lib", "HOME=/tmp", "LANG=C", "LC_ALL=C", "PATH=/nonexistent"}, "WorkingDir": "/tmp",
		"HostConfig": map[string]any{"NetworkMode": "none", "ReadonlyRootfs": true, "Privileged": false, "CapDrop": []any{"ALL"}, "SecurityOpt": []any{"no-new-privileges"}, "PidsLimit": json.Number("128"), "Memory": json.Number("805306368"), "NanoCpus": json.Number("1000000000"), "RestartPolicy": map[string]any{"Name": "no"}, "LogConfig": noLog(), "Tmpfs": map[string]any{"/tmp": "rw,nosuid,nodev,noexec,size=134217728,mode=1777"}},
	}, nil
}

func postgresTemplate(name string, init bool, uid, gid uint32) (map[string]any, error) {
	hosts := map[string]string{"app": "172.30.80.2", "w1": "172.30.82.2", "w2": "172.30.82.3", "control": "172.30.82.4"}
	host, ok := hosts[name]
	if !ok || uid == 0 || gid == 0 {
		return nil, errGuardProtocol
	}
	mounts := []any{bind("/var/lib/g2/"+name+"/pgdata", "/var/lib/postgresql/data", false)}
	executable := "/usr/lib/postgresql/16/bin/postgres"
	args := []any{"-D", "/var/lib/postgresql/data", "-c", "listen_addresses=0.0.0.0", "-c", "port=5432", "-c", "unix_socket_directories=", "-c", "archive_mode=off", "-c", "archive_command=", "-c", "max_wal_senders=0", "-c", "max_replication_slots=0", "-c", "wal_level=replica", "-c", "logging_collector=off"}
	if init {
		executable = "/usr/lib/postgresql/16/bin/initdb"
		args = []any{"-D", "/var/lib/postgresql/data", "-U", "postgres", "--pwfile=/run/g2-postgres-secret", "--auth-local=reject", "--auth-host=scram-sha-256", "--encoding=UTF8", "--locale=C.UTF-8"}
		mounts = append(mounts, bind("/run/g2/pg-secret-"+name, "/run/g2-postgres-secret", true))
	}
	network, project, service := "g2-ledgers", "g2domains", name
	if name == "app" {
		network, project, service = "g2-initial-app", "g2app", "db"
	}
	netMode := network
	if init {
		netMode = "none"
	}
	out := map[string]any{
		"Image": postgresImageID, "Entrypoint": []any{executable}, "Cmd": args,
		"Labels": map[string]any{"com.docker.compose.project": project, "com.docker.compose.service": service, "com.docker.compose.project.working_dir": "/opt/g2/compose"},
		"User":   fmt.Sprintf("%d:%d", uid, gid), "WorkingDir": "/var/lib/postgresql/data",
		"Env":        []any{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/lib/postgresql/16/bin", "LANG=C.UTF-8"},
		"HostConfig": map[string]any{"NetworkMode": netMode, "Dns": []any{"127.0.0.1"}, "ReadonlyRootfs": true, "Privileged": false, "CapDrop": []any{"ALL"}, "SecurityOpt": []any{"no-new-privileges"}, "PidsLimit": json.Number("256"), "RestartPolicy": map[string]any{"Name": "no"}, "LogConfig": noLog(), "Mounts": mounts},
	}
	if !init {
		out["NetworkingConfig"] = map[string]any{"EndpointsConfig": map[string]any{network: map[string]any{"IPAMConfig": map[string]any{"IPv4Address": host}}}}
	}
	return out, nil
}
