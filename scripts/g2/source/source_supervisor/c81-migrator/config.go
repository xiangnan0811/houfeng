//go:build linux && !cgo

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type applicationIdentity struct {
	id               string
	systemIdentifier string
	databaseName     string
	databaseOID      uint32
	epoch            uint64
}

type privateConfig struct {
	phase       uint64
	migratorURL string
	runtimeRole string
	adminRole   string
	workerRole  string
	application applicationIdentity
}

// strictObject is used for both levels of the private config and every ACK.
// Unmarshal into a struct alone would silently accept duplicate JSON keys.
func strictObject(body []byte, names ...string) (map[string]json.RawMessage, error) {
	if len(body) == 0 || len(body) > privateLimit || !utf8.Valid(body) {
		return nil, denied
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, denied
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || !allowed[name] || fields[name] != nil {
			return nil, denied
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, denied
		}
		fields[name] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(fields) != len(names) {
		return nil, denied
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, denied
	}
	return fields, nil
}

func readConfig() (privateConfig, error) {
	defer unix.Close(4)
	var st unix.Stat_t
	flags, flagErr := unix.FcntlInt(4, unix.F_GETFL, 0)
	seals, sealErr := unix.FcntlInt(4, unix.F_GET_SEALS, 0)
	link, linkErr := os.Readlink("/proc/self/fd/4")
	required := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	if unix.Fstat(4, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size <= 0 || st.Size > privateLimit ||
		flagErr != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || sealErr != nil || seals&required != required ||
		linkErr != nil || !strings.HasPrefix(link, "/memfd:") || !strings.HasSuffix(link, " (deleted)") {
		return privateConfig{}, denied
	}
	body := make([]byte, int(st.Size))
	defer clear(body)
	for offset := 0; offset < len(body); {
		n, err := unix.Pread(4, body[offset:], int64(offset))
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return privateConfig{}, denied
		}
		offset += n
	}
	fields, err := strictObject(body, "version", "phase_generation", "migrator_url", "runtime_role", "admin_role", "worker_role", "application")
	if err != nil {
		return privateConfig{}, denied
	}
	var result privateConfig
	var version uint64
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 ||
		json.Unmarshal(fields["phase_generation"], &result.phase) != nil || result.phase == 0 ||
		json.Unmarshal(fields["migrator_url"], &result.migratorURL) != nil || len(result.migratorURL) == 0 || len(result.migratorURL) > 8192 ||
		json.Unmarshal(fields["runtime_role"], &result.runtimeRole) != nil ||
		json.Unmarshal(fields["admin_role"], &result.adminRole) != nil ||
		json.Unmarshal(fields["worker_role"], &result.workerRole) != nil {
		return privateConfig{}, denied
	}
	app, err := strictObject(fields["application"], "ID", "SystemIdentifier", "DatabaseName", "DatabaseOID", "Epoch")
	if err != nil || json.Unmarshal(app["ID"], &result.application.id) != nil ||
		json.Unmarshal(app["SystemIdentifier"], &result.application.systemIdentifier) != nil ||
		json.Unmarshal(app["DatabaseName"], &result.application.databaseName) != nil ||
		json.Unmarshal(app["DatabaseOID"], &result.application.databaseOID) != nil ||
		json.Unmarshal(app["Epoch"], &result.application.epoch) != nil || !validApplication(result.application) {
		return privateConfig{}, denied
	}
	return result, nil
}

func validApplication(app applicationIdentity) bool {
	if len(app.id) != 67 || !strings.HasPrefix(app.id, "rd-") || app.databaseOID == 0 ||
		app.epoch == 0 || app.epoch > math.MaxInt64 || len(app.databaseName) == 0 || len(app.databaseName) > 63 ||
		!utf8.ValidString(app.databaseName) {
		return false
	}
	raw, err := hex.DecodeString(app.id[3:])
	if err != nil || hex.EncodeToString(raw) != app.id[3:] {
		return false
	}
	system, err := strconv.ParseUint(app.systemIdentifier, 10, 64)
	if err != nil || system == 0 || strconv.FormatUint(system, 10) != app.systemIdentifier {
		return false
	}
	for _, ch := range app.databaseName {
		if ch == '/' || ch == 0 || unicode.IsControl(ch) {
			return false
		}
	}
	return true
}
