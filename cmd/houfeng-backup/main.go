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

	"houfeng/internal/center/recordbackup"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("houfeng-backup failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: houfeng-backup plan|create|verify [--profile local|s3]")
	}
	command := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("houfeng-backup", flag.ContinueOnError)
	profile := flags.String("profile", "local", "backup profile: local or s3")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	service, err := recordbackup.NewService(recordbackup.Options{
		Build: recordbackup.BuildIdentity{Profile: recordbackup.Profile(*profile)},
	})
	if err != nil {
		switch command {
		case "plan", "create", "verify":
			return fmt.Errorf("%w: %s", recordbackup.ErrBackupUnavailable, command)
		default:
			return fmt.Errorf("unknown command %q", command)
		}
	}
	_ = ctx
	_ = service
	return fmt.Errorf("%w: %s", recordbackup.ErrBackupUnavailable, command)
}
