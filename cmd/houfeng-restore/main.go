package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"houfeng/internal/center/recordrestore"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("houfeng-restore failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: houfeng-restore plan|apply|verify [--profile local|s3]")
	}
	command := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("houfeng-restore", flag.ContinueOnError)
	profile := flags.String("profile", "local", "restore profile: local or s3")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	_ = profile

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	service, err := recordrestore.NewService(recordrestore.Options{})
	if err != nil {
		switch command {
		case "plan", "apply", "verify":
			return fmt.Errorf("%w: %s", recordrestore.ErrRestoreUnavailable, command)
		default:
			return fmt.Errorf("unknown command %q", command)
		}
	}
	_ = ctx
	_ = service
	return fmt.Errorf("%w: %s", recordrestore.ErrRestoreUnavailable, command)
}
