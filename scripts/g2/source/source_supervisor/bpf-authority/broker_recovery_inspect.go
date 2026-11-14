//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
)

func recoveryDockerGET(daemon *daemonRegistration, path string, deadline time.Time) (map[string]any, error) {
	if !strings.HasPrefix(path, "/v1.41/") || strings.ContainsAny(path, "\r\n ") {
		return nil, errGuardProtocol
	}
	conn, err := connectDocker(daemon, deadline)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	message := []byte("GET " + path + " HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	for len(message) > 0 {
		n, e := conn.Write(message)
		if e != nil {
			return nil, e
		}
		if n <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		message = message[n:]
	}
	reader := bufio.NewReaderSize(conn, 8192)
	headers, err := boundedDockerHeaders(reader)
	if err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(headers), reader)), &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errGuardProtocol
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errGuardProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	value, err := readJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errGuardProtocol
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, errGuardProtocol
	}
	return result, nil
}

func recoveryEnvironment(value any) (map[string]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errGuardProtocol
	}
	out := make(map[string]string, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, errGuardProtocol
		}
		key, val, ok := strings.Cut(text, "=")
		if !ok || key == "" || strings.ContainsRune(key, 0) || strings.ContainsRune(val, 0) {
			return nil, errGuardProtocol
		}
		if _, exists := out[key]; exists {
			return nil, errGuardProtocol
		}
		out[key] = val
	}
	return out, nil
}

// inspectRecoveryContainer independently reads the authenticated Docker daemon,
// requiring the original container to be stopped and its effective settings to
// match the prearmed pinned configuration before a fresh grant can be made.
func inspectRecoveryContainer(daemon *daemonRegistration, entry recoveryContainer, deadline time.Time) error {
	if !runtimeContainerID(entry.id) || entry.config == nil {
		return errGuardProtocol
	}
	image, err := recoveryDockerGET(daemon, "/v1.41/images/"+postgresImageID+"/json", deadline)
	if err != nil || image["Id"] != postgresImageID {
		return errGuardProtocol
	}
	imageConfig, ok := image["Config"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	env, err := recoveryEnvironment(imageConfig["Env"])
	if err != nil {
		return err
	}
	overrides, err := recoveryEnvironment(entry.config["Env"])
	if err != nil {
		return err
	}
	for k, v := range overrides {
		env[k] = v
	}
	item, err := recoveryDockerGET(daemon, "/v1.41/containers/"+entry.id+"/json", deadline)
	if err != nil || item["Id"] != entry.id || item["Image"] != postgresImageID {
		return errGuardProtocol
	}
	state, ok := item["State"].(map[string]any)
	if !ok || state["Running"] != false || state["Pid"] != json.Number("0") {
		return errGuardProtocol
	}
	actual, ok := item["Config"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	for _, key := range []string{"User", "Entrypoint", "Cmd", "WorkingDir", "Labels"} {
		if !sameJSON(actual[key], entry.config[key]) {
			return errGuardProtocol
		}
	}
	actualEnv, err := recoveryEnvironment(actual["Env"])
	if err != nil || !reflect.DeepEqual(actualEnv, env) {
		return errGuardProtocol
	}
	host, ok := item["HostConfig"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	expected, ok := entry.config["HostConfig"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	for _, key := range []string{"NetworkMode", "Dns", "ReadonlyRootfs", "Privileged", "PidsLimit"} {
		if !sameJSON(host[key], expected[key]) {
			return errGuardProtocol
		}
	}
	if !sameJSON(host["CapDrop"], []any{"ALL"}) || !sameJSON(host["SecurityOpt"], []any{"no-new-privileges"}) || !nilOrEmptyArray(host["Devices"]) || !nilOrEmptyArray(host["DeviceRequests"]) || !nilOrEmptyArray(host["DeviceCgroupRules"]) || host["CgroupParent"] != "" {
		return errGuardProtocol
	}
	restart, ok := host["RestartPolicy"].(map[string]any)
	if !ok || restart["Name"] != "no" {
		return errGuardProtocol
	}
	log, ok := host["LogConfig"].(map[string]any)
	if !ok || log["Type"] != "none" {
		return errGuardProtocol
	}
	ports, ok := host["PortBindings"].(map[string]any)
	if host["PortBindings"] != nil && !ok {
		return errGuardProtocol
	}
	for _, value := range ports {
		if !nilOrEmptyArray(value) {
			return errGuardProtocol
		}
	}
	network, ok := item["NetworkSettings"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	networkPorts, ok := network["Ports"].(map[string]any)
	if network["Ports"] != nil && !ok {
		return errGuardProtocol
	}
	for _, value := range networkPorts {
		if !nilOrEmptyArray(value) {
			return errGuardProtocol
		}
	}
	mounts, ok := item["Mounts"].([]any)
	if !ok || len(mounts) != 1 {
		return errGuardProtocol
	}
	mount, ok := mounts[0].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	wanted, ok := expected["Mounts"].([]any)
	if !ok || len(wanted) != 1 {
		return errGuardProtocol
	}
	wm, ok := wanted[0].(map[string]any)
	if !ok || mount["Type"] != "bind" || mount["Source"] != wm["Source"] || mount["Destination"] != wm["Target"] || mount["RW"] != true {
		return errGuardProtocol
	}
	return nil
}
