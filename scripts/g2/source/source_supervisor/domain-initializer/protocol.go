//go:build linux

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

const privateLimit = 16 << 10

var errPrivateInput = errors.New("invalid private input")

type endpoint struct {
	host     string
	port     uint16
	database string
}

type bootstrapConnection struct {
	endpoint endpoint
	role     string
	password string
}

type credentials struct {
	w1Reader            string
	w1Writer            string
	w2RecordReader      string
	w2RecordWriter      string
	w2SourceReader      string
	w2SourceProducer    string
	w2SourceProvisioner string
}

type privateInput struct {
	w1          bootstrapConnection
	w2          bootstrapConnection
	control     bootstrapConnection
	credentials credentials
}

// The entire request must be a sealed memfd, independently of its offset.
func readInput() (privateInput, error) {
	var input privateInput
	file := os.NewFile(4, "domain-initializer-config")
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
	fields, err := strictFields(body, "version", "w1_bootstrap_url", "w2_bootstrap_url", "control_bootstrap_url", "credentials")
	if err != nil {
		return input, errPrivateInput
	}
	var version uint64
	var w1URL, w2URL, controlURL string
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 ||
		json.Unmarshal(fields["w1_bootstrap_url"], &w1URL) != nil ||
		json.Unmarshal(fields["w2_bootstrap_url"], &w2URL) != nil ||
		json.Unmarshal(fields["control_bootstrap_url"], &controlURL) != nil {
		return input, errPrivateInput
	}
	credentialFields, err := strictFields(fields["credentials"], "w1_reader", "w1_writer", "w2_record_reader", "w2_record_writer", "w2_source_reader", "w2_source_producer", "w2_source_provisioner")
	if err != nil ||
		json.Unmarshal(credentialFields["w1_reader"], &input.credentials.w1Reader) != nil ||
		json.Unmarshal(credentialFields["w1_writer"], &input.credentials.w1Writer) != nil ||
		json.Unmarshal(credentialFields["w2_record_reader"], &input.credentials.w2RecordReader) != nil ||
		json.Unmarshal(credentialFields["w2_record_writer"], &input.credentials.w2RecordWriter) != nil ||
		json.Unmarshal(credentialFields["w2_source_reader"], &input.credentials.w2SourceReader) != nil ||
		json.Unmarshal(credentialFields["w2_source_producer"], &input.credentials.w2SourceProducer) != nil ||
		json.Unmarshal(credentialFields["w2_source_provisioner"], &input.credentials.w2SourceProvisioner) != nil {
		return privateInput{}, errPrivateInput
	}
	if input.w1, err = parseBootstrapURL(w1URL, "g2_w1"); err != nil {
		return privateInput{}, errPrivateInput
	}
	if input.w2, err = parseBootstrapURL(w2URL, "g2_w2"); err != nil {
		return privateInput{}, errPrivateInput
	}
	if input.control, err = parseBootstrapURL(controlURL, "g2_control"); err != nil {
		return privateInput{}, errPrivateInput
	}
	passwords := []string{input.w1.password, input.w2.password, input.control.password,
		input.credentials.w1Reader, input.credentials.w1Writer,
		input.credentials.w2RecordReader, input.credentials.w2RecordWriter,
		input.credentials.w2SourceReader, input.credentials.w2SourceProducer, input.credentials.w2SourceProvisioner}
	seen := make(map[string]struct{}, len(passwords))
	for _, password := range passwords {
		if password == "" || strings.ContainsRune(password, '\x00') {
			return privateInput{}, errPrivateInput
		}
		if _, found := seen[password]; found {
			return privateInput{}, errPrivateInput
		}
		seen[password] = struct{}{}
	}
	return input, nil
}

// Both objects have an exact field set; duplicate keys, nulls, and trailing
// JSON values are rejected before any typed decoding.
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

// No hostnames, implicit ports, socket paths, failover hosts, or connection
// options are admitted. Root separately binds this endpoint to its fixed VM.
func parseBootstrapURL(raw, database string) (bootstrapConnection, error) {
	var result bootstrapConnection
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		parsed.Opaque != "" || parsed.User == nil || parsed.Fragment != "" ||
		parsed.Path != "/"+database || parsed.RawPath != "" || parsed.ForceQuery || parsed.RawQuery != "sslmode=disable" {
		return result, errPrivateInput
	}
	password, present := parsed.User.Password()
	role := parsed.User.Username()
	if !present || password == "" || role == "" || strings.ContainsAny(role, "\x00\r\n") || strings.ContainsRune(password, '\x00') {
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
	result.endpoint = endpoint{host: host, port: uint16(port), database: database}
	result.role, result.password = role, password
	return result, nil
}

func connectionURL(connection bootstrapConnection) string {
	uri := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(connection.role, connection.password),
		Host:   net.JoinHostPort(connection.endpoint.host, strconv.Itoa(int(connection.endpoint.port))),
		Path:   "/" + connection.endpoint.database,
	}
	query := url.Values{}
	query.Set("sslmode", "disable")
	query.Set("passfile", "/dev/null")
	query.Set("connect_timeout", "5")
	query.Set("application_name", "g2-domain-initializer")
	uri.RawQuery = query.Encode()
	return uri.String()
}
