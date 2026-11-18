//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func inspectOwnerProbe(reg *daemonRegistration, cid, purpose string, deadline time.Time, diagnostic *admitDiagnostic) error {
	if !runtimeContainerID(cid) {
		return errGuardProtocol
	}
	conn, err := connectDocker(reg, deadline)
	if err != nil {
		return err
	}
	defer conn.Close()
	message := []byte("GET /v1.41/containers/" + cid + "/json HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	for len(message) > 0 {
		n, e := conn.Write(message)
		if e != nil {
			return e
		}
		if n <= 0 {
			return io.ErrUnexpectedEOF
		}
		message = message[n:]
	}
	reader := bufio.NewReaderSize(conn, 8192)
	headers, err, observed := boundedDockerHeadersDiagnostic(reader)
	diagnostic.observed = observed
	if err != nil {
		return err
	}
	response, err := http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(headers), reader)), &http.Request{Method: "GET"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errGuardProtocol
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		diagnostic.observed = err
	}
	if err != nil || len(raw) > 1<<20 {
		return errGuardProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoded, err := readJSONValue(decoder, 0)
	if err != nil {
		return err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return errGuardProtocol
	}
	item, ok := decoded.(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	return validateOwnerProbeInspection(item, cid, purpose)
}

func validateOwnerProbeInspection(item map[string]any, cid, purpose string) error {
	if item["Id"] != cid || item["Image"] != postgresImageID {
		return errGuardProtocol
	}
	state, ok := item["State"].(map[string]any)
	if !ok || state["Status"] != "created" || state["Running"] != false || state["Pid"] != json.Number("0") {
		return errGuardProtocol
	}
	config, ok := item["Config"].(map[string]any)
	if !ok || config["Tty"] != false || config["AttachStdout"] != true || config["AttachStderr"] != true {
		return errGuardProtocol
	}
	flag := "-u"
	if purpose == "postgres-gid" {
		flag = "-g"
	} else if purpose != "postgres-uid" {
		return errGuardProtocol
	}
	if !sameJSON(config["Entrypoint"], []any{"/usr/bin/id"}) || !sameJSON(config["Cmd"], []any{flag, "postgres"}) {
		return errGuardProtocol
	}
	host, ok := item["HostConfig"].(map[string]any)
	if !ok || host["NetworkMode"] != "none" || host["ReadonlyRootfs"] != true || host["Privileged"] != false || !sameJSON(host["CapDrop"], []any{"ALL"}) || !sameJSON(host["SecurityOpt"], []any{"no-new-privileges"}) || !nilOrEmptyArray(host["Binds"]) || !sameJSON(host["Mounts"], []any{bind(ownerProbeSourcePath, ownerProbeTargetPath, true)}) {
		return errGuardProtocol
	}
	mounts, ok := item["Mounts"].([]any)
	if !ok || len(mounts) != 1 {
		return errGuardProtocol
	}
	mount, ok := mounts[0].(map[string]any)
	if !ok || mount["Type"] != "bind" || mount["Source"] != ownerProbeSourcePath || mount["Destination"] != ownerProbeTargetPath || mount["RW"] != false {
		return errGuardProtocol
	}
	if propagation, present := mount["Propagation"]; present && propagation != "" && propagation != "rprivate" {
		return errGuardProtocol
	}
	if name := mount["Name"]; name != nil && name != "" {
		return errGuardProtocol
	}
	return nil
}

func sameJSON(a, b any) bool {
	x, err := canonicalControl(a)
	if err != nil {
		return false
	}
	y, err := canonicalControl(b)
	return err == nil && bytes.Equal(x, y)
}
func nilOrEmptyArray(v any) bool {
	if v == nil {
		return true
	}
	a, ok := v.([]any)
	return ok && len(a) == 0
}

func (b *authorityBroker) templateForPrincipal(p *controlPrincipal, purpose string) (map[string]any, error) {
	if b == nil || p == nil {
		return nil, errGuardProtocol
	}
	if p.Role == "BOOT_OWNER" {
		switch purpose {
		case "postgres-uid", "postgres-gid":
			return ownerProbeConfig(purpose)
		case "dns-preflight":
			return dnsProbeTemplate(), nil
		default:
			return nil, errControlFrame
		}
	}
	if p.Role == "REALM_ROOT" {
		if purpose == "container-bpf-selftest" {
			if b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" {
				return nil, errControlFrame
			}
			variant := b.caseFacts["bpf_variant"]
			if variant != nil && variant != "" && variant != "baseline" {
				return nil, errControlFrame
			}
			template := containerBPFSelftestTemplate()
			if template == nil {
				return nil, errGuardProtocol
			}
			return template, nil
		}
		if purpose == "browser-negative" {
			if b.caseFacts == nil || b.caseFacts["scenario"] != "extra-browser" {
				return nil, errControlFrame
			}
			return browserTemplate(b.browserImage)
		}
		for _, prefix := range []string{"postgres-init-", "postgres-live-"} {
			if strings.HasPrefix(purpose, prefix) {
				return postgresTemplate(strings.TrimPrefix(purpose, prefix), prefix == "postgres-init-", b.postgresUID, b.postgresGID)
			}
		}
	}
	if p.Role == "RECOVERY_OBSERVER" && b.recovery != nil && strings.HasPrefix(purpose, "recovery-") {
		entry, ok := b.recovery.containers[strings.TrimPrefix(purpose, "recovery-")]
		if !ok {
			return nil, errControlFrame
		}
		return entry.config, nil
	}
	return nil, errControlFrame
}
