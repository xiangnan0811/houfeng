package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const (
	frameLimit    = 16 << 10
	runtimeRole   = "houfeng_runtime"
	applicationDB = "houfeng"
)

var errProtocol = errors.New("private protocol unavailable")

type privateInput struct {
	Version      uint64
	RuntimeURL   string
	DeploymentID string
	ActorUserID  string
	RecordID     string
}

type endpoint struct {
	host     string
	port     uint16
	password string
}

func readInput() (privateInput, endpoint, error) {
	var input privateInput
	file := os.NewFile(4, "c80-comparison-negative-config")
	if file == nil {
		return input, endpoint{}, errProtocol
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > frameLimit {
		return input, endpoint{}, errProtocol
	}
	link, err := os.Readlink("/proc/self/fd/4")
	if err != nil || !strings.HasPrefix(link, "/memfd:") || !strings.HasSuffix(link, " (deleted)") {
		return input, endpoint{}, errProtocol
	}
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	required := unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if err != nil || seals&required != required {
		return input, endpoint{}, errProtocol
	}
	body := make([]byte, int(info.Size()))
	if n, err := file.ReadAt(body, 0); err != nil || n != len(body) {
		return input, endpoint{}, errProtocol
	}
	fields, err := strictFields(body, "version", "runtime_url", "deployment_id", "actor_user_id", "record_id")
	if err != nil || json.Unmarshal(fields["version"], &input.Version) != nil ||
		json.Unmarshal(fields["runtime_url"], &input.RuntimeURL) != nil ||
		json.Unmarshal(fields["deployment_id"], &input.DeploymentID) != nil ||
		json.Unmarshal(fields["actor_user_id"], &input.ActorUserID) != nil ||
		json.Unmarshal(fields["record_id"], &input.RecordID) != nil || input.Version != 1 ||
		recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(input.DeploymentID)) != nil ||
		recordauth.ValidateActorUserID(input.ActorUserID) != nil || !records.ValidRecordRootID(input.RecordID) {
		return privateInput{}, endpoint{}, errProtocol
	}
	ep, err := parseRuntimeURL(input.RuntimeURL)
	if err != nil {
		return privateInput{}, endpoint{}, errProtocol
	}
	input.RuntimeURL = ""
	return input, ep, nil
}

// Only top-level fixed fields are accepted; the caller decodes each value by
// its exact type. In particular, duplicate keys and JSON null are never defaults.
func strictFields(body []byte, names ...string) (map[string]json.RawMessage, error) {
	if len(body) == 0 || len(body) > frameLimit || !utf8.Valid(body) {
		return nil, errProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errProtocol
	}
	expected := make(map[string]bool, len(names))
	for _, name := range names {
		expected[name] = true
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !expected[key] || fields[key] != nil {
			return nil, errProtocol
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, errProtocol
		}
		fields[key] = raw
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != len(expected) {
		return nil, errProtocol
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, errProtocol
	}
	return fields, nil
}

// The root supervisor independently binds this fixed private endpoint to APP.
// No hostnames, libpq parameters, alternate hosts, or indirect credentials enter
// the connection configuration.
func parseRuntimeURL(raw string) (endpoint, error) {
	var result endpoint
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		parsed.Opaque != "" || parsed.User == nil || parsed.Fragment != "" ||
		parsed.Path != "/"+applicationDB || parsed.RawPath != "" || parsed.ForceQuery {
		return result, errProtocol
	}
	password, present := parsed.User.Password()
	if !present || len(password) < 32 || strings.ContainsAny(password, "\x00\r\n") || parsed.User.Username() != runtimeRole {
		return result, errProtocol
	}
	host := parsed.Hostname()
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || !ip.IsPrivate() || ip.String() != host {
		return result, errProtocol
	}
	portText := parsed.Port()
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port != 5432 || strconv.FormatUint(port, 10) != portText ||
		parsed.Host != net.JoinHostPort(host, portText) {
		return result, errProtocol
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" {
		return result, errProtocol
	}
	return endpoint{host: host, port: uint16(port), password: password}, nil
}

func connectionURL(ep endpoint) string {
	uri := url.URL{
		Scheme: "postgres", User: url.UserPassword(runtimeRole, ep.password),
		Host: net.JoinHostPort(ep.host, strconv.Itoa(int(ep.port))), Path: "/" + applicationDB,
	}
	query := url.Values{}
	query.Set("sslmode", "disable")
	query.Set("passfile", "/dev/null")
	query.Set("connect_timeout", "5")
	query.Set("application_name", "g2-c80-comparison-negative")
	uri.RawQuery = query.Encode()
	return uri.String()
}
