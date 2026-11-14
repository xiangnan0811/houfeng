package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func main() {
	// pgx must not consume PGHOST, PGOPTIONS, PGSERVICE, passfiles, or any
	// other inherited environment setting. All connection values come from
	// the sealed input and the verified, fixed authority state.
	os.Clearenv()
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "c80 initializer:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 || (os.Args[1] != "initialize" && os.Args[1] != "heartbeat") || !validCase(os.Args[2]) {
		return stageError("invalid_invocation")
	}
	input, err := readInput(os.Args[2], os.Args[1])
	if err != nil {
		return stageError("private_input_unavailable")
	}
	connectionURL := input.BootstrapURL
	if os.Args[1] == "heartbeat" {
		connectionURL = input.AuthorityURL
	}
	connection, err := parseBootstrapURL(connectionURL)
	if err != nil || (os.Args[1] == "heartbeat" && connection.role != authorityRole) {
		return stageError("private_input_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	encoder := json.NewEncoder(os.Stdout)
	if os.Args[1] == "initialize" {
		result, err := initialize(ctx, input, connection)
		if err != nil {
			return err
		}
		if err := encoder.Encode(result); err != nil {
			return stageError("output_unavailable")
		}
		return nil
	}
	result, err := refreshHeartbeat(ctx, input, connection)
	if err != nil {
		return err
	}
	if err := encoder.Encode(result); err != nil {
		return stageError("output_unavailable")
	}
	return nil
}
