package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	frameLimit = 16 << 10
	dumpLimit  = 64 << 20
)

var errInput = errors.New("private input unavailable")

type request struct {
	version    uint64
	mode       string
	dumpSize   uint64
	dumpDigest [32]byte
}

func readRequest() (request, error) {
	var req request
	file := os.NewFile(4, "c80-backup-negative-config")
	if file == nil {
		return req, errInput
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > frameLimit || !sealedMemfd(file) {
		return req, errInput
	}
	body := make([]byte, int(info.Size()))
	if n, err := file.ReadAt(body, 0); err != nil || n != len(body) {
		return req, errInput
	}
	fields, err := strictFields(body, "version", "mode", "dump_size", "dump_sha256")
	var digestText string
	if err != nil || json.Unmarshal(fields["version"], &req.version) != nil ||
		json.Unmarshal(fields["mode"], &req.mode) != nil || json.Unmarshal(fields["dump_size"], &req.dumpSize) != nil ||
		json.Unmarshal(fields["dump_sha256"], &digestText) != nil || req.version != 1 ||
		(req.mode != "staging" && req.mode != "published") || req.dumpSize == 0 || req.dumpSize > dumpLimit ||
		len(digestText) != 64 {
		return request{}, errInput
	}
	digest, err := hex.DecodeString(digestText)
	if err != nil || len(digest) != len(req.dumpDigest) || hex.EncodeToString(digest) != digestText {
		return request{}, errInput
	}
	copy(req.dumpDigest[:], digest)
	if req.dumpDigest == ([32]byte{}) {
		return request{}, errInput
	}
	return req, nil
}

func sealedMemfd(file *os.File) bool {
	link, err := os.Readlink("/proc/self/fd/" + strconv.FormatUint(uint64(file.Fd()), 10))
	if err != nil || !strings.HasPrefix(link, "/memfd:") || !strings.HasSuffix(link, " (deleted)") {
		return false
	}
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	required := unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	return err == nil && seals&required == required
}

func strictFields(body []byte, names ...string) (map[string]json.RawMessage, error) {
	if len(body) == 0 || len(body) > frameLimit || !utf8.Valid(body) {
		return nil, errInput
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errInput
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
			return nil, errInput
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, errInput
		}
		fields[key] = raw
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != len(expected) {
		return nil, errInput
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, errInput
	}
	return fields, nil
}
