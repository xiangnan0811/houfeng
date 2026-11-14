//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recorddeletion"
)

type domainOutput struct {
	W1      recorddeletion.LedgerPhysicalIdentity `json:"w1"`
	W2      recorddeletion.LedgerPhysicalIdentity `json:"w2"`
	Control platformmigrate.DomainIdentity        `json:"control"`
}

type initializeOutput struct {
	Version uint64       `json:"version"`
	Domains domainOutput `json:"domains"`
}

type stageError string

func (e stageError) Error() string { return string(e) }

func main() {
	// Prevent pgx from consulting inherited PG*, service, or home settings.
	os.Clearenv()
	if err := run(); err != nil {
		// Only fixed stage labels may leave this process; underlying errors can
		// contain credentials, statements, endpoints, or database content.
		_, _ = fmt.Fprintln(os.Stderr, "domain initializer:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 || os.Args[1] != "initialize" ||
		os.Getuid() != 10002 || os.Geteuid() != 10002 || os.Getgid() != 10002 || os.Getegid() != 10002 {
		return stageError("invalid_invocation")
	}
	input, err := readInput()
	if err != nil {
		return stageError("private_input_unavailable")
	}
	const deadline = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	// The existing maintenance provisioning API uses a background context.
	// Bound the whole one-shot process as well as the context-aware DB calls.
	watchdog := time.AfterFunc(deadline, func() {
		_, _ = fmt.Fprintln(os.Stderr, "domain initializer: deadline_unavailable")
		os.Exit(1)
	})
	defer watchdog.Stop()
	result, err := initialize(ctx, input)
	if err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return stageError("output_unavailable")
	}
	data = append(data, '\n')
	if n, err := os.Stdout.Write(data); err != nil || n != len(data) {
		return stageError("output_unavailable")
	}
	return nil
}
