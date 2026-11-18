package main

import (
	"context"
	"errors"
	"path/filepath"

	"houfeng/internal/center/deploy"
)

// The root host controller consumes a protected, retry-stable request file.
func runOrdinaryComposeUpgrade(ctx context.Context, args []string) error {
	if len(args) != 3 || args[0] != "deploy-ordinary-upgrade" || args[1] != "--request-file" || !filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] {
		return errors.New("deploy-ordinary-upgrade requires --request-file with an absolute protected path")
	}
	return deploy.RunOrdinaryComposeUpgrade(ctx, args[2])
}
