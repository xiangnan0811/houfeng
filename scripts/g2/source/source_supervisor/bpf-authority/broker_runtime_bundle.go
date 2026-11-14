//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func runtimeJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > runtimeInputLimit {
		return nil, errControlFrame
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, errControlFrame
	}
	out, ok := value.(map[string]any)
	if !ok {
		return nil, errControlFrame
	}
	return out, nil
}

// recheckRuntimeBundle compares the retained original descriptor with a
// fresh no-follow traversal and exact content after every later invocation.
func recheckRuntimeBundle(grant *brokerGrant) error {
	if grant == nil || !grant.createBound || grant.facts == nil || !grant.facts.runtimeReady || grant.bundleDirFD < 0 || grant.bundleConfigFD < 0 || grant.bundlePath == "" || grant.bundleSHA == "" {
		return errGuardProtocol
	}
	dup, err := unix.FcntlInt(uintptr(grant.bundleConfigFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(dup), "config.json")
	if file == nil {
		_ = unix.Close(dup)
		return errGuardProtocol
	}
	defer file.Close()
	return runtimeRecheckInput(grant.bundleDirFD, grant.bundlePath, "config.json", file, grant.bundleSHA, grant.bundleStat, false)
}
