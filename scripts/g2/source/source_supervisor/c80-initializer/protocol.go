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
)

const (
	privateLimit  = 16 << 10
	applicationDB = "houfeng"
	runtimeRole   = "houfeng_runtime"
	adminRole     = "houfeng_platform_admin"
	migratorRole  = "houfeng_migrator"
	authorityRoot = "/var/lib/g2/control/maintenance/authority"
	intersectingSourceFixture = "intersecting-source-closure"
)

var errPrivateInput = errors.New("invalid private input")

type privateInput struct {
	Version          uint64
	Case             string
	BootstrapURL     string
	AuthorityURL     string
	RuntimePassword  string
	AdminPassword    string
	MigratorPassword string
	FixtureVariant   string
}

type endpoint struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Database string `json:"database"`
}

type bootstrapConnection struct {
	endpoint endpoint
	role     string
	password string
}

// readInput accepts only a complete sealed memfd, independently of its file
// offset. Neither a pipe nor an ordinary file can serve as private input.
func readInput(caseName, mode string) (privateInput, error) {
	var input privateInput
	file := os.NewFile(4, "c80-initializer-config")
	if file == nil {
		return input, errPrivateInput
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > privateLimit {
		return input, errPrivateInput
	}
	link, err := os.Readlink("/proc/self/fd/4")
	if err != nil || !strings.HasPrefix(link, "/memfd:") || !strings.HasSuffix(link, " (deleted)") {
		return input, errPrivateInput
	}
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	required := unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if err != nil || seals&required != required {
		return input, errPrivateInput
	}
	body := make([]byte, int(info.Size()))
	if n, err := file.ReadAt(body, 0); err != nil || n != len(body) {
		return input, errPrivateInput
	}
	if mode == "heartbeat" {
		fields, err := strictFields(body, "version", "case", "authority_url")
		if err != nil || json.Unmarshal(fields["version"], &input.Version) != nil ||
			json.Unmarshal(fields["case"], &input.Case) != nil ||
			json.Unmarshal(fields["authority_url"], &input.AuthorityURL) != nil ||
			input.Version != 1 || input.Case != caseName || !validCase(input.Case) || input.AuthorityURL == "" {
			return privateInput{}, errPrivateInput
		}
		return input, nil
	}
	if mode != "initialize" {
		return privateInput{}, errPrivateInput
	}
	fields, err := strictFields(body, "version", "case", "app_bootstrap_url", "runtime_password", "admin_password", "migrator_password")
	if err != nil {
		fields, err = strictFields(body, "version", "case", "app_bootstrap_url", "runtime_password", "admin_password", "migrator_password", "fixture_variant")
		if err != nil || json.Unmarshal(fields["fixture_variant"], &input.FixtureVariant) != nil || input.FixtureVariant != intersectingSourceFixture {
			return privateInput{}, errPrivateInput
		}
	}
	if err != nil || json.Unmarshal(fields["version"], &input.Version) != nil ||
		json.Unmarshal(fields["case"], &input.Case) != nil ||
		json.Unmarshal(fields["app_bootstrap_url"], &input.BootstrapURL) != nil ||
		json.Unmarshal(fields["runtime_password"], &input.RuntimePassword) != nil ||
		json.Unmarshal(fields["admin_password"], &input.AdminPassword) != nil ||
		json.Unmarshal(fields["migrator_password"], &input.MigratorPassword) != nil ||
		input.Version != 1 || input.Case != caseName || !validCase(input.Case) ||
		(input.FixtureVariant != "" && input.Case != "source-vps") ||
		input.BootstrapURL == "" || input.RuntimePassword == "" || input.AdminPassword == "" || input.MigratorPassword == "" {
		return privateInput{}, errPrivateInput
	}
	return input, nil
}

func validCase(name string) bool {
	switch name {
	case "source-vps", "source-monitoring-instance", "source-target", "source-recovery":
		return true
	default:
		return false
	}
}

// strictFields rejects duplicate, unknown, missing and null top-level fields,
// and rejects trailing JSON values. Each field is subsequently decoded by type.
func strictFields(body []byte, names ...string) (map[string]json.RawMessage, error) {
	if len(body) == 0 || len(body) > privateLimit || !utf8.Valid(body) {
		return nil, errPrivateInput
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errPrivateInput
	}
	expected := make(map[string]bool, len(names))
	for _, name := range names {
		expected[name] = true
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		item, err := decoder.Token()
		key, ok := item.(string)
		if err != nil || !ok || !expected[key] || fields[key] != nil {
			return nil, errPrivateInput
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, errPrivateInput
		}
		fields[key] = raw
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != len(expected) {
		return nil, errPrivateInput
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, errPrivateInput
	}
	return fields, nil
}

// The supervisor binds this private IPv4 endpoint to the actual APP container
// and its current bridge. URL options cannot introduce a fallback connection.
func parseBootstrapURL(raw string) (bootstrapConnection, error) {
	var result bootstrapConnection
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		parsed.Opaque != "" || parsed.User == nil || parsed.Fragment != "" ||
		parsed.Path != "/"+applicationDB || parsed.RawPath != "" || parsed.ForceQuery {
		return result, errPrivateInput
	}
	password, present := parsed.User.Password()
	role := parsed.User.Username()
	if !present || password == "" || role == "" || strings.ContainsAny(role, "\x00\r\n") {
		return result, errPrivateInput
	}
	host := parsed.Hostname()
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || !ip.IsPrivate() || ip.String() != host {
		return result, errPrivateInput
	}
	portText := parsed.Port()
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port != 5432 || strconv.FormatUint(port, 10) != portText ||
		parsed.Host != net.JoinHostPort(host, portText) {
		return result, errPrivateInput
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" {
		return result, errPrivateInput
	}
	result.endpoint = endpoint{Host: host, Port: uint16(port), Database: applicationDB}
	result.role, result.password = role, password
	return result, nil
}

func connectionURL(ep endpoint, role, password string) string {
	uri := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(role, password),
		Host:   net.JoinHostPort(ep.Host, strconv.Itoa(int(ep.Port))),
		Path:   "/" + applicationDB,
	}
	query := url.Values{}
	query.Set("sslmode", "disable")
	query.Set("passfile", "/dev/null")
	query.Set("connect_timeout", "5")
	query.Set("application_name", "g2-c80-initializer")
	uri.RawQuery = query.Encode()
	return uri.String()
}
